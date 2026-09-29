package tui

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/grubless/grubless-cli/go/internal/golden"
)

// Expectations produced by src/tui/. States are serialised by the TS and
// decoded here, so both builds render and reduce the same screens. The TS
// clock was pinned to this instant while generating them.
var fixedNow = time.Date(2026, 8, 9, 4, 0, 0, 0, time.UTC)

func TestKeysAndWidthsMatchTS(t *testing.T) {
	var g struct {
		DecodeKeys [][2]json.RawMessage `json:"decodeKeys"`
		Truncate   [][4]json.RawMessage `json:"truncate"`
	}
	golden.Load(t, "testdata/terminal.json.gz", &g)

	for _, c := range g.DecodeKeys {
		var chunk string
		var want []struct {
			Name string `json:"name"`
			Ctrl bool   `json:"ctrl"`
		}
		_ = json.Unmarshal(c[0], &chunk)
		_ = json.Unmarshal(c[1], &want)
		got := DecodeKeys(chunk)
		if len(got) != len(want) {
			t.Errorf("DecodeKeys(%q) = %v, want %v", chunk, got, want)
			continue
		}
		for i := range got {
			if got[i].Name != want[i].Name || got[i].Ctrl != want[i].Ctrl {
				t.Errorf("DecodeKeys(%q)[%d] = %v, want %v", chunk, i, got[i], want[i])
			}
		}
	}
	for _, c := range g.Truncate {
		var text, want string
		var width, visible int
		_ = json.Unmarshal(c[0], &text)
		_ = json.Unmarshal(c[1], &width)
		_ = json.Unmarshal(c[2], &want)
		_ = json.Unmarshal(c[3], &visible)
		if got := Truncate(text, width); got != want {
			t.Errorf("Truncate(%q, %d) = %q, want %q", text, width, got, want)
		}
		if got := VisibleWidth(text); got != visible {
			t.Errorf("VisibleWidth(%q) = %d, want %d", text, got, visible)
		}
	}
}

func TestRenderMatchesTS(t *testing.T) {
	Now = func() time.Time { return fixedNow }
	defer func() { Now = time.Now }()

	var screens []struct {
		State  State    `json:"state"`
		Width  int      `json:"width"`
		Height int      `json:"height"`
		Lines  []string `json:"lines"`
	}
	golden.Load(t, "testdata/render.json.gz", &screens)

	for i, s := range screens {
		got := Render(s.State, s.Width, s.Height)
		if slices.Equal(got, s.Lines) {
			continue
		}
		t.Errorf("screen #%d (%dx%d, tab %s):", i, s.Width, s.Height, s.State.Tab)
		for j := 0; j < max(len(got), len(s.Lines)); j++ {
			var a, b string
			if j < len(got) {
				a = got[j]
			}
			if j < len(s.Lines) {
				b = s.Lines[j]
			}
			if a != b {
				t.Errorf("  line %d\n   got: %q\n  want: %q", j, a, b)
			}
		}
	}
}

func TestReduceMatchesTS(t *testing.T) {
	type projection struct {
		EntityIndex int     `json:"entityIndex"`
		Selected    *string `json:"selected"`
		Tab         Tab     `json:"tab"`
		Offset      int     `json:"offset"`
		Cursor      int     `json:"cursor"`
		Loading     bool    `json:"loading"`
		Message     *string `json:"message"`
		MessageKind string  `json:"messageKind"`
		Syncing     bool    `json:"syncing"`
		ShowHelp    bool    `json:"showHelp"`
		Quit        bool    `json:"quit"`
		Range       string  `json:"range"`
		Holdings    int     `json:"holdings"`
	}
	var sequences []struct {
		Start    State `json:"start"`
		Viewport int   `json:"viewport"`
		Steps    []struct {
			Key struct {
				Name string `json:"name"`
				Ctrl bool   `json:"ctrl"`
			} `json:"key"`
			Action Action     `json:"action"`
			State  projection `json:"state"`
		} `json:"steps"`
	}
	golden.Load(t, "testdata/reduce.json.gz", &sequences)

	project := func(s State) projection {
		p := projection{
			EntityIndex: s.EntityIndex, Tab: s.Tab, Offset: s.Offset, Cursor: s.Cursor, Loading: s.Loading,
			Message: s.Message, MessageKind: s.MessageKind, Syncing: s.Syncing, ShowHelp: s.ShowHelp,
			Quit: s.Quit, Range: string(s.Range), Holdings: len(s.Data.Holdings),
		}
		if s.SelectedEntity != nil {
			p.Selected = &s.SelectedEntity.ID
		}
		return p
	}
	str := func(p *string) string {
		if p == nil {
			return "<nil>"
		}
		return *p
	}

	for i, seq := range sequences {
		state := seq.Start
		for j, step := range seq.Steps {
			next, action := Reduce(state, Key{Name: step.Key.Name, Ctrl: step.Key.Ctrl}, seq.Viewport)
			got, want := project(next), step.State
			if action != step.Action || str(got.Selected) != str(want.Selected) || str(got.Message) != str(want.Message) {
				t.Fatalf("sequence #%d step %d (%v): action %s selected %s message %s, want %s %s %s",
					i, j, step.Key, action, str(got.Selected), str(got.Message), step.Action, str(want.Selected), str(want.Message))
			}
			got.Selected, want.Selected, got.Message, want.Message = nil, nil, nil, nil
			if got != want {
				t.Fatalf("sequence #%d step %d (%v):\n got: %+v\nwant: %+v", i, j, step.Key, got, want)
			}
			state = next
		}
	}
}
