package main

import (
	"cmp"
	"strings"

	"github.com/egoist/mygo/ui"
)

type fieldKind uint8

const (
	fieldText fieldKind = iota
	fieldArea
	fieldCheck
	fieldSelect
	fieldNote // a line of explanation
	fieldRadio
)

// field is a control of a dialog.
type field struct {
	kind        fieldKind
	key         string
	label       string
	text        string
	checked     bool
	options     []string
	hints       []string // under each option of a radio
	lines       int      // the height of an area, in lines
	placeholder string
	mono        bool
}

// dialog is a sheet over the window with a few fields and two buttons, as
// Fork's operations (Create Branch, Push, Reset…) are.
type dialog struct {
	title   string
	message string
	fields  []*field
	ok      string
	// danger makes the confirm button a warning, for what cannot be undone.
	danger bool
	// info is a message with an OK button alone.
	info bool
	// run does the work, off the main thread; its error shows in the sheet.
	run func(d *dialog) error
	// validate disables the confirm button, with the reason as its tooltip.
	validate func(d *dialog) string
	// done runs on the main thread after run succeeded.
	done func()

	open bool
	busy bool
	err  string
	// focused is set once the first field took the focus.
	focused bool
}

func (d *dialog) field(key string) *field {
	for _, f := range d.fields {
		if f.key == key {
			return f
		}
	}
	return &field{}
}

// str is the text of a field, trimmed.
func (d *dialog) str(key string) string { return strings.TrimSpace(d.field(key).text) }

// raw is the text of a field as typed.
func (d *dialog) raw(key string) string { return d.field(key).text }

func (d *dialog) bool(key string) bool { return d.field(key).checked }

func text(key, label, placeholder, value string) *field {
	return &field{kind: fieldText, key: key, label: label, placeholder: placeholder, text: value}
}

func check(key, label string, on bool) *field {
	return &field{kind: fieldCheck, key: key, label: label, checked: on}
}

func choice(key, label, value string, options ...string) *field {
	return &field{kind: fieldSelect, key: key, label: label, text: value, options: options}
}

func area(key, label, placeholder, value string) *field {
	return &field{kind: fieldArea, key: key, label: label, placeholder: placeholder, text: value, lines: 6}
}

// option is a choice of a radio field, with what it does.
type option struct{ label, hint string }

// radio is a choice among options shown in full, each with its hint.
func radio(key, value string, options ...option) *field {
	f := &field{kind: fieldRadio, key: key, text: value}
	for _, o := range options {
		f.options = append(f.options, o.label)
		f.hints = append(f.hints, o.hint)
	}
	return f
}

func note(s string) *field { return &field{kind: fieldNote, label: s} }

// showDialog opens a dialog.
func (a *app) showDialog(d *dialog) {
	d.open = true
	if d.ok == "" {
		d.ok = "OK"
	}
	a.dialog = d
}

// confirm asks before doing something that loses work.
func (a *app) confirm(title, message, ok string, run func() error, done func()) {
	a.showDialog(&dialog{title: title, message: message, ok: ok, danger: true, run: func(*dialog) error { return run() }, done: done})
}

// dialogView builds the open dialog.
func (a *app) dialogView(c *ui.Context) {
	d := a.dialog
	if d == nil {
		return
	}
	t := c.Theme()
	if !d.open {
		a.dialog = nil
		return
	}
	submit := false
	cancel := false
	reason := ""
	if d.validate != nil {
		reason = d.validate(d)
	}
	modal := ui.Modal(c, &d.open, func() {
		ui.Column(c).Width(440).Gap(12).Padding(4).Children(func() {
			ui.Text(c, d.title).FontSize(15).Bold()
			if d.message != "" {
				ui.Text(c, d.message).FontSize(13).TextColor(t.TextMuted).Selectable()
			}
			for i, f := range d.fields {
				a.fieldView(c, d, f, i == 0)
			}
			if d.err != "" {
				ui.Text(c, d.err).FontSize(12).TextColor(t.Danger).Selectable()
			}
			ui.Row(c).Gap(8).Justify(ui.End).AlignItems(ui.Center).Children(func() {
				if d.busy {
					ui.Spinner(c).Label("Working")
				}
				if !d.info {
					if ui.Button(c, "Cancel").Disabled(d.busy).Clicked() {
						cancel = true
					}
				}
				var btn ui.Element
				if d.danger {
					btn = ui.Button(c, d.ok)
				} else {
					btn = ui.PrimaryButton(c, d.ok)
				}
				btn.Disabled(d.busy || reason != "")
				if reason != "" {
					btn.Tooltip(reason)
				}
				if btn.Clicked() {
					submit = true
				}
			})
		})
	})
	if modal.Dismissed() && d.busy {
		d.open = true
	}
	if cancel {
		d.open = false
	}
	if submit {
		a.submitDialog(d)
	}
}

func (a *app) fieldView(c *ui.Context, d *dialog, f *field, first bool) {
	t := c.Theme()
	switch f.kind {
	case fieldNote:
		ui.Text(c, f.label).FontSize(12).TextColor(t.TextMuted)
	case fieldCheck:
		ui.Checkbox(c, &f.checked, f.label)
	case fieldSelect:
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, f.label).Width(110).TextAlign(ui.End).TextColor(t.TextMuted).FontSize(13)
			ui.Select(c, &f.text, f.options).Grow(1)
		})
	case fieldRadio:
		ui.RadioGroup(c, func() {
			for i, o := range f.options {
				on := f.text == o
				row := ui.RadioBase(c.Key("radio"+o), &f.text, o).Gap(12).Padding(6, 4).AlignItems(ui.Start).Label(o)
				row.Children(func() {
					ring := ui.Box(c).Size(22, 22).Radius(11).Shrink(0).Center().Border(2, hairline)
					if on {
						ring.Background(t.Accent).BorderColor(t.Accent)
					}
					ring.Children(func() {
						if on {
							ui.Box(c).Size(8, 8).Radius(4).Background(t.AccentText)
						}
					})
					ui.Column(c).Grow(1).MinWidth(0).Children(func() {
						ui.Text(c, o).FontSize(14).FontWeight(600)
						ui.Text(c, f.hints[i]).FontSize(12.5).TextColor(t.TextMuted)
					})
				})
			}
		})
	case fieldArea:
		ui.Column(c).Gap(4).Children(func() {
			if f.label != "" {
				ui.Text(c, f.label).FontSize(12).TextColor(t.TextMuted)
			}
			in := ui.TextArea(c, &f.text).Lines(cmp.Or(f.lines, 3), max(f.lines, 8)).Placeholder(f.placeholder).Label(cmp.Or(f.label, f.placeholder))
			if first && !d.focused {
				in.AutoFocus()
			}
		})
	default:
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, f.label).Width(110).TextAlign(ui.End).TextColor(t.TextMuted).FontSize(13)
			in := ui.TextInput(c, &f.text).Placeholder(f.placeholder).Grow(1).Label(f.label)
			if f.mono {
				in.Font("SF Mono, Menlo, monospace")
			}
			if first && !d.focused {
				in.AutoFocus()
			}
			if in.Submitted() {
				a.submitDialog(d)
			}
		})
	}
	if first {
		d.focused = true
	}
}

// submitDialog does what the dialog asks.
func (a *app) submitDialog(d *dialog) {
	if d.busy {
		return
	}
	if d.validate != nil && d.validate(d) != "" {
		return
	}
	if d.run == nil {
		d.open = false
		if d.done != nil {
			d.done()
		}
		return
	}
	d.busy, d.err = true, ""
	a.background(func() {
		err := d.run(d)
		a.update(func() {
			d.busy = false
			if err != nil {
				d.err = errorText(err)
				a.refresh()
				return
			}
			d.open = false
			if d.done != nil {
				d.done()
			}
			a.refresh()
		})
	})
}
