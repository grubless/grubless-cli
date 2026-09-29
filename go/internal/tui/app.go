package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/grubless/grubless-cli/go/internal/api"
	"github.com/grubless/grubless-cli/go/internal/client"
	"github.com/grubless/grubless-cli/go/internal/config"
	"github.com/grubless/grubless-cli/go/internal/output"
	"github.com/grubless/grubless-cli/go/internal/txfmt"
)

// The event loop: the impure shell around the pure reducer and views. Fetch,
// paint, dispatch keys. See src/tui/app.ts.
//
// Node gives the TS a single-threaded event loop for free; here it's made
// explicit. Everything that touches `state` runs on the loop goroutine, as a
// func sent down `events` — key input, timers and finished fetches alike —
// so there are no locks and no races, and the ordering is the TS's ordering.

const (
	syncPoll = 2 * time.Second
	// Fast enough to read as motion, slow enough not to strobe.
	spinnerInterval = 120 * time.Millisecond
)

type app struct {
	client  *client.Client
	term    *Terminal
	state   State
	events  chan func()
	spinner *time.Ticker
	stopped chan struct{}
	code    int
	done    bool
	// themeFromEnv: GRUBLESS_THEME chose the theme, so a switch with `t`
	// can't outlast the session, and says so.
	themeFromEnv bool
}

// Run opens the interface and blocks until the user quits.
func Run(c *client.Client) (int, error) {
	if !output.IsTerminal(os.Stdin) || !output.IsTerminal(os.Stdout) {
		// A TUI written into a pipe emits escapes as data.
		return 0, output.Errorf(output.UsageError,
			"The interactive interface needs a terminal.\nIn a script or CI, use the commands directly — run `grubless --help`.")
	}

	a := &app{client: c, term: NewTerminal(), state: InitialState(), events: make(chan func(), 64), stopped: make(chan struct{})}
	theme, fromEnv := config.Theme()
	name, ok := NormaliseTheme(theme)
	if name == "" {
		name = DefaultTheme()
	}
	if !ok {
		// Said before the screen is taken over, where it can still be read.
		source := "the config file"
		if fromEnv {
			source = "GRUBLESS_THEME"
		}
		output.Note(fmt.Sprintf("warning: unknown theme %q in %s; using %s. Themes: %s.", theme, source, ThemeLabel[name], strings.Join(Themes, ", ")))
	}
	a.state.Theme, a.state.TrueColour, a.themeFromEnv = name, TrueColour(), fromEnv
	if err := a.term.Open(paletteOf(a.state).base); err != nil {
		return 0, err
	}
	// Restore the terminal on every exit path, including a panic — a TUI
	// that leaves raw mode on hands back a shell that looks broken.
	defer a.term.Close()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)
	stopResize := watchResize(func() { a.post(a.onResize) })
	defer stopResize()

	go a.readKeys()

	a.syncSpinner()
	a.paint()
	a.loadEntities()

	for {
		select {
		case fn := <-a.events:
			fn()
			if a.done {
				a.stopSpinner()
				close(a.stopped)
				return a.code, nil
			}
		case <-a.tickerC():
			a.state.Tick++
			a.paint()
		case <-signals:
			a.term.Close()
			os.Exit(130)
		}
	}
}

// post schedules fn on the loop. Safe from any goroutine; dropped once the
// app has finished, so a late fetch can't block forever.
func (a *app) post(fn func()) {
	select {
	case a.events <- fn:
	case <-a.stopped:
	}
}

func (a *app) paint() { a.term.Render(Render(a.state, a.term.Width(), a.term.Height())) }

func (a *app) onResize() {
	a.term.InvalidateFrame()
	a.paint()
}

// tickerC is the spinner's channel, or nil (which never fires) when idle.
func (a *app) tickerC() <-chan time.Time {
	if a.spinner == nil {
		return nil
	}
	return a.spinner.C
}

// syncSpinner runs the spinner only while something is loading: waking every
// 120ms to redraw an idle screen is a battery cost for nothing.
func (a *app) syncSpinner() {
	if a.state.Loading && a.spinner == nil {
		a.spinner = time.NewTicker(spinnerInterval)
	} else if !a.state.Loading {
		a.stopSpinner()
	}
}

func (a *app) stopSpinner() {
	if a.spinner != nil {
		a.spinner.Stop()
		a.spinner = nil
	}
}

func (a *app) setState(next State) {
	a.state = next
	a.syncSpinner()
	a.paint()
}

// fail shows the first line of an error in the status bar.
func (a *app) fail(err error) {
	next := a.state
	next.Loading, next.Syncing = false, false
	next.Message = msg(strings.SplitN(err.Error(), "\n", 2)[0])
	next.MessageKind = "error"
	a.setState(next)
}

// readKeys decodes stdin on its own goroutine and posts keys to the loop.
// A read can end mid-way through a UTF-8 character; the tail is carried to
// the next read, as Node's string decoder does.
func (a *app) readKeys() {
	buf := make([]byte, 1024)
	var pending []byte
	for {
		n, err := os.Stdin.Read(buf)
		if n > 0 {
			pending = append(pending, buf[:n]...)
			cut := len(pending)
			for i := len(pending) - 1; i >= 0 && i >= len(pending)-utf8.UTFMax; i-- {
				if utf8.RuneStart(pending[i]) {
					if !utf8.FullRune(pending[i:]) {
						cut = i // an incomplete character: wait for the rest
					}
					break
				}
			}
			chunk := string(pending[:cut])
			pending = append([]byte(nil), pending[cut:]...)
			a.post(func() {
				for _, key := range DecodeKeys(chunk) {
					if a.done {
						return
					}
					a.onKey(key)
				}
			})
		}
		if err != nil {
			return
		}
	}
}

func (a *app) onKey(key Key) {
	next, action := Reduce(a.state, key, ViewportRows(a.term.Height()))
	a.setState(next)

	switch action {
	case ActQuit:
		a.code, a.done = output.Ok, true
	case ActOpenEntity, ActReload:
		if next.SelectedEntity != nil {
			a.loadEntityData(*next.SelectedEntity)
		}
	case ActSync:
		if next.SelectedEntity != nil {
			a.startSync(*next.SelectedEntity)
		}
	case ActTheme:
		a.term.SetBase(paletteOf(next).base)
		message := "Theme: " + ThemeLabel[next.Theme]
		switch err := config.SaveTheme(next.Theme); {
		case err != nil:
			message += " (not saved: " + err.Error() + ")"
		case a.themeFromEnv:
			message += " — GRUBLESS_THEME will override it next time"
		}
		next.Message = msg(message)
		a.setState(next)
	case ActApplyFilter:
		if next.SelectedEntity != nil {
			a.applyFilter(*next.SelectedEntity, next.TxFilter)
		}
	case ActLoadMore:
		if next.SelectedEntity != nil && next.Data.TxNextCursor != nil {
			a.loadMoreTransactions(*next.SelectedEntity, *next.Data.TxNextCursor)
		}
	}
}

func (a *app) loadEntities() {
	go func() {
		entities, err := getJSON[[]api.Entity](a.client, "/entities")
		a.post(func() {
			if err != nil {
				a.fail(err)
				return
			}
			next := a.state
			next.Entities = entities
			next.Loading = false
			a.setState(next)
		})
	}()
}

func getJSON[T any](c *client.Client, path string) (T, error) {
	var zero T
	raw, err := c.GetRaw(path)
	if err != nil {
		return zero, err
	}
	v, err := api.Decode[T](raw)
	if err != nil {
		return zero, fmt.Errorf("GET %s: %w", path, err)
	}
	return v, nil
}

// loadEntityData is one round of parallel fetches rather than per-tab lazy
// loading: switching tabs to cross-check a figure shouldn't mean a spinner
// each time. Like Promise.all, the first failure to arrive is the one shown.
func (a *app) loadEntityData(entity api.Entity) {
	base := "/entities/" + entity.ID
	var d EntityData
	type part struct {
		apply func()
		err   error
	}
	parts := make(chan part, 9)
	fetchInto := func(path string, into func([]byte) error) {
		go func() {
			raw, err := a.client.GetRaw(path)
			if err == nil {
				err = into(raw)
			}
			parts <- part{err: err}
		}()
	}
	fetchInto(base+"/holdings", into(&d.Holdings))
	fetchInto(base+"/sources", into(&d.Sources))
	fetchInto(base+"/tax-summary", into(&d.Tax))
	fetchInto(base+"/warnings/zero-cost", into(&d.ZeroCost))
	fetchInto(base+"/warnings/uncategorized-transfers", into(&d.Uncategorized))
	fetchInto(base+"/activity", into(&d.Activity))
	fetchInto(base+"/portfolio-history", into(&d.History))
	fetchInto(base+"/tx-events?"+a.state.TxFilter.Query(txPageSize, ""), func(raw []byte) error {
		page, err := api.Decode[api.TxPage](raw)
		if err != nil {
			return err
		}
		d.Transactions, d.TxNextCursor = page.Events, emptyToNil(page.NextCursor)
		d.TxAssets = txfmt.Assets{}
		d.TxAssets.Add(page.Assets)
		return nil
	})
	// Settings 404 on an entity without a row, which isn't worth failing the
	// screen over: the chart falls back to July and nothing else cares.
	go func() {
		settings, err := getJSON[api.EntityTaxSettings](a.client, base+"/tax-settings")
		if err == nil {
			d.Settings = &settings
		}
		parts <- part{}
	}()

	go func() {
		for i := 0; i < 9; i++ {
			if p := <-parts; p.err != nil {
				err := p.err
				a.post(func() { a.fail(err) })
				return
			}
		}
		a.post(func() {
			next := a.state
			next.Data = d
			next.Loading = false
			a.setState(next)
		})
	}()
}

// into decodes a response body into one field of the data being loaded.
func into[T any](dst *T) func([]byte) error {
	return func(raw []byte) error {
		v, err := api.Decode[T](raw)
		*dst = v
		return err
	}
}

// txPageSize is small next to the API's 2000: the tab only needs a
// screenful to start, and loads the next page before you reach the end.
const txPageSize = 100

func emptyToNil(p *string) *string {
	if p == nil || *p == "" {
		return nil
	}
	return p
}

// loadMoreTransactions appends the page after `cursor`. If the list was
// reloaded or the entity changed while it was in flight, the page belongs to
// a list that no longer exists and is dropped.
func (a *app) loadMoreTransactions(entity api.Entity, cursor string) {
	filter := a.state.TxFilter
	go func() {
		page, err := getJSON[api.TxPage](a.client, "/entities/"+entity.ID+"/tx-events?"+filter.Query(txPageSize, cursor))
		a.post(func() {
			next := a.state
			current := next.Data.TxNextCursor
			if next.SelectedEntity == nil || next.SelectedEntity.ID != entity.ID || current == nil || *current != cursor {
				return
			}
			next.Data.TxLoadingMore = false
			if err != nil {
				a.state = next
				a.fail(err)
				return
			}
			next.Data.Transactions = append(append([]api.TxEvent(nil), next.Data.Transactions...), page.Events...)
			assets := txfmt.Assets{}
			for id, asset := range next.Data.TxAssets {
				assets[id] = asset
			}
			assets.Add(page.Assets)
			next.Data.TxAssets = assets
			next.Data.TxNextCursor = emptyToNil(page.NextCursor)
			a.setState(next)
		})
	}()
}

// applyFilter refetches the transactions from their first page. If the
// filter or the entity changed again before it answered, the answer is for a
// question nobody is asking any more, and is dropped.
func (a *app) applyFilter(entity api.Entity, filter TxFilter) {
	go func() {
		page, err := getJSON[api.TxPage](a.client, "/entities/"+entity.ID+"/tx-events?"+filter.Query(txPageSize, ""))
		a.post(func() {
			next := a.state
			if next.SelectedEntity == nil || next.SelectedEntity.ID != entity.ID || next.TxFilter != filter {
				return
			}
			if err != nil {
				a.fail(filterError(err))
				return
			}
			next.Data.Transactions, next.Data.TxNextCursor = page.Events, emptyToNil(page.NextCursor)
			assets := txfmt.Assets{}
			for id, asset := range next.Data.TxAssets {
				assets[id] = asset
			}
			assets.Add(page.Assets)
			next.Data.TxAssets = assets
			next.Loading = false
			a.setState(next)
		})
	}()
}

// filterError makes the API's rejection of a filter readable on one line:
// the status bar shows only a message's first line, and for a validation
// error that line is just "GET … rejected:". The field errors follow it.
func filterError(err error) error {
	msg := err.Error()
	_, detail, found := strings.Cut(msg, "rejected:\n")
	if !found {
		return err
	}
	var body struct {
		FieldErrors map[string][]string `json:"fieldErrors"`
	}
	if json.Unmarshal([]byte(detail), &body) != nil || len(body.FieldErrors) == 0 {
		return err
	}
	var parts []string
	for field, problems := range body.FieldErrors {
		parts = append(parts, field+": "+strings.Join(problems, "; "))
	}
	sort.Strings(parts)
	return fmt.Errorf("The API rejected the filter — %s", strings.Join(parts, " · "))
}

func (a *app) startSync(entity api.Entity) {
	var sources []api.Source
	for _, s := range a.state.Data.Sources {
		if s.SyncEnabled {
			sources = append(sources, s)
		}
	}
	if len(sources) == 0 {
		next := a.state
		next.Syncing = false
		next.Message = msg("No syncable sources.")
		next.MessageKind = "info"
		a.setState(next)
		return
	}
	go func() {
		for _, s := range sources {
			// skipPriceBackfill across a multi-source run, as in the command.
			if err := a.client.Post("/sources/"+s.ID+"/sync", map[string]bool{"full": false, "skipPriceBackfill": len(sources) > 1}); err != nil {
				a.post(func() { a.fail(err) })
				return
			}
		}
		a.post(func() {
			next := a.state
			next.Message = msg(fmt.Sprintf("Queued %d source(s)…", len(sources)))
			next.MessageKind = "info"
			a.setState(next)
			a.watchSync(entity)
		})
	}()
}

// watchSync polls activity while a sync runs, showing the worker's own
// progress text — the one thing a TUI does better than the scriptable path.
// It re-observes; it never re-triggers.
func (a *app) watchSync(entity api.Entity) {
	time.AfterFunc(syncPoll, func() {
		activity, err := getJSON[[]api.EntityActivity](a.client, "/entities/"+entity.ID+"/activity")
		a.post(func() {
			if err != nil {
				a.fail(err)
				return
			}
			var running, failed []api.EntityActivity
			for _, act := range activity {
				switch act.Status {
				case "running":
					running = append(running, act)
				case "error":
					failed = append(failed, act)
				}
			}
			next := a.state
			next.Data.Activity = activity
			if len(running) > 0 {
				next.Message = msg("Syncing…")
				if running[0].Message != nil {
					next.Message = msg(*running[0].Message)
				}
				next.MessageKind = "info"
				a.setState(next)
				a.watchSync(entity)
				return
			}
			next.Syncing = false
			if len(failed) > 0 {
				next.Message = msg(fmt.Sprintf("%d source(s) failed", len(failed)))
				next.MessageKind = "error"
			} else {
				next.Message = msg("Sync finished")
				next.MessageKind = "success"
			}
			a.setState(next)
			// Figures will have moved; stale numbers on screen are the worst
			// outcome for a tool people check figures with.
			a.loadEntityData(entity)
		})
	})
}
