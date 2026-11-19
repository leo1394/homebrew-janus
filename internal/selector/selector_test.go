//go:build !windows

package selector

import (
	"bytes"
	"strings"
	"testing"
)

func TestConvenKeysAndLayout(t *testing.T) {
	state := newSinglePickerState([]Candidate{{Name:"Phone", Path:"serial-1", Detail:"android"}, {Name:"Builder", Path:"terminal:2", Detail:"terminal · alice@host:22"}}, Prompt{Title:"Select device", EmptySelectionNotice:"Select one device before confirming."})
	state.handle(key{kind:keyEnter})
	if state.mode != modePicking || state.notice == "" { t.Fatal("empty selection accepted") }
	state.handle(key{kind:keyRune, rune:'f'})
	state.handle(key{kind:keyRune, rune:'j'})
	var output bytes.Buffer
	if err := renderPicker(&output, state, 120, 24); err != nil { t.Fatal(err) }
	for _, text := range []string{"Select device\r\n\r\n", "  [✔︎] Phone  serial-1 · android", "> [ ] Builder  terminal:2 · terminal · alice@host:22", "Showing 1-2 of 2 · selected 1", "[f/Space] selection · [Enter] confirm · [q/Esc] quit"} {
		if !strings.Contains(output.String(), text) { t.Fatalf("missing %q in %q",text,output.String()) }
	}
	state.handle(key{kind:keyRune, rune:'F'})
	if state.selected[0] || !state.selected[1] || state.cursor != 1 { t.Fatal("single selection or bounded movement changed") }
	state.handle(key{kind:keyEnter})
	if state.mode != modeConfirming || state.selectedCandidates()[0].Path != "terminal:2" { t.Fatal("Enter did not submit selected device") }
}

func TestInstallMultiSelectionUsesConvenKeys(t *testing.T) {
	state := newPickerStateWithOptions([]Candidate{{Name:"Android", Path:"a"}, {Name:"iPhone", Path:"b"}}, Prompt{Title:"Select devices to install"}, selectionMany)
	state.handle(key{kind:keyRune, rune:'F'})
	state.handle(key{kind:keyRune, rune:'f'})
	if state.selectedCount() != 2 { t.Fatal("multi-selection did not preserve both devices") }
	var output bytes.Buffer
	if err := renderPicker(&output, state, 120, 24); err != nil { t.Fatal(err) }
	if !strings.Contains(output.String(), "[f/Space|A] selection") { t.Fatal("missing multi-select controls") }
	state.handle(key{kind:keyEnter})
	if state.mode != modeConfirming || len(state.selectedCandidates()) != 2 { t.Fatal("selection not submitted") }
}

func TestDisabledDevicesCannotBeSelectedIndividuallyOrWithSelectAll(t *testing.T) {
	state := newPickerStateWithOptions([]Candidate{{Name:"Offline", Disabled:true}, {Name:"Online"}, {Name:"iPhone"}}, Prompt{}, selectionMany)
	state.handle(key{kind:keyRune, rune:'f'})
	if state.selectedCount() != 0 || state.notice == "" { t.Fatal("disabled device selected") }
	state.handle(key{kind:keyRune, rune:'A'})
	if state.selected[0] || state.selectedCount() != 2 { t.Fatal("select all included disabled device") }
	state.handle(key{kind:keyRune, rune:'A'})
	if state.selectedCount() != 0 { t.Fatal("deselect all failed") }
}

func TestSpaceTogglesSelection(t *testing.T) {
	state := newSinglePickerState([]Candidate{{Name:"Online"}, {Name:"Offline", Disabled:true}}, Prompt{})
	state.handle(key{kind:keyRune, rune:' '})
	if state.selectedCount() != 1 { t.Fatal("space did not select") }
	state.handle(key{kind:keyRune, rune:' '})
	if state.selectedCount() != 0 { t.Fatal("space did not deselect") }
	state.handle(key{kind:keyDown})
	state.handle(key{kind:keyRune, rune:' '})
	if state.selectedCount() != 0 { t.Fatal("space selected unavailable device") }
}
