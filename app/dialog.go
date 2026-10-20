package app

import (
	"cmp"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

type fieldKind uint8

const (
	fieldText fieldKind = iota
	fieldArea
	fieldCheck
	fieldSelect
	fieldNote // a line of explanation
	fieldRadio
	fieldCombo  // a field to type in, which narrows a list to choose from
	fieldAction // a button that does something to the other fields
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
	// act is what an action does, off the main thread; the function it returns
	// changes the fields, on the main thread.
	act  func(d *dialog) (apply func(), err error)
	busy bool // an action is running
	// show says whether the field is there, for what depends on the others;
	// labelFn, when set, is the label, for the same reason.
	show    func(d *dialog) bool
	labelFn func(d *dialog) string
	// preview gives an area a Raw and a Preview of its text as markdown; tab
	// is the one shown.
	preview bool
	tab     int
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

	// width is the dialog's, in points; 0 is the usual 440.
	width float32
	// stacked puts each label over its field, which is the width of the dialog,
	// instead of beside a field that takes what is left.
	stacked bool

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

// combo is a choice among many options, found by typing: the value is one of
// them.
func combo(key, label, value string, options ...string) *field {
	return &field{kind: fieldCombo, key: key, label: label, text: value, options: options}
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

// action is a button that writes into the dialog's fields: what it does is
// act's, and busy says it is going.
func action(label string, act func(d *dialog) (func(), error)) *field {
	return &field{kind: fieldAction, label: label, act: act}
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
// With a window it is the native alert of the system; without one, as in
// tests, it is the dialog drawn in the app.
func (a *app) confirm(title, message, ok string, run func() error, done func()) {
	if a.win != nil {
		go func() {
			res, err := mygo.Dialog.Message(mygo.MessageOptions{
				Parent: a.win, Type: mygo.MessageWarning, Message: title, Detail: message,
				Buttons: []string{"Cancel", ok}, DefaultButton: 0, CancelButton: 0,
			})
			if err != nil || res.Button != 1 {
				return
			}
			err = run()
			a.update(func() {
				if err != nil {
					a.say("%s", errorText(err))
				} else if done != nil {
					done()
				}
				a.refresh()
			})
		}()
		return
	}
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
		ui.Column(c).Width(cmp.Or(d.width, 440)).Gap(12).Padding(4).Children(func() {
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
	if f.show != nil && !f.show(d) {
		return
	}
	label := f.label
	if f.labelFn != nil {
		label = f.labelFn(d)
	}
	// labeled puts the label beside the control, or over it when the dialog is
	// stacked, where the control takes the whole width.
	labeled := func(control func(stacked bool)) {
		if d.stacked {
			ui.Column(c).Gap(4).FillWidth().Children(func() {
				ui.Text(c, label).FontSize(12).TextColor(t.TextMuted)
				control(true)
			})
			return
		}
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Text(c, label).Width(110).TextAlign(ui.End).TextColor(t.TextMuted).FontSize(13)
			control(false)
		})
	}
	fit := func(e ui.Element, stacked bool) ui.Element {
		if stacked {
			return e.FillWidth()
		}
		return e.Grow(1)
	}
	switch f.kind {
	case fieldNote:
		ui.Text(c, label).FontSize(12).TextColor(t.TextMuted)
	case fieldAction:
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			if ui.Button(c, f.label).Disabled(f.busy || d.busy).Clicked() {
				a.runFieldAction(d, f)
			}
			if f.busy {
				ui.Spinner(c).Label("Writing")
			}
		})
	case fieldCheck:
		ui.Checkbox(c, &f.checked, label)
	case fieldSelect:
		labeled(func(stacked bool) { fit(ui.Select(c, &f.text, f.options), stacked) })
	case fieldCombo:
		labeled(func(stacked bool) { fit(ui.Combobox(c, &f.text, f.options).Label(f.label), stacked) })
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
		ui.Column(c).Gap(4).FillWidth().Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				if label != "" {
					ui.Text(c, label).FontSize(12).TextColor(t.TextMuted)
				}
				if f.preview {
					ui.Spacer(c)
					ui.Segmented(c.Key("preview-"+f.key), &f.tab, "Raw", "Preview").Label(cmp.Or(label, "Text") + " view")
				}
			})
			if f.preview && f.tab == 1 {
				ui.Column(c).FillWidth().MinHeight(float32(cmp.Or(f.lines, 3))*20).MaxHeight(320).Padding(8, 10).Radius(8).Border(1, hairline).Children(func() {
					ui.Scroll(c).Grow(1).MinHeight(0).Children(func() {
						if strings.TrimSpace(f.text) == "" {
							ui.Text(c, "Nothing to preview").FontSize(13).TextColor(t.TextMuted)
						} else {
							markdown.View(c, f.text, &markdown.Options{OnLink: openInBrowser}).FillWidth()
						}
					})
				})
				return
			}
			in := ui.TextArea(c, &f.text).Lines(cmp.Or(f.lines, 3), max(f.lines, 8)).Placeholder(f.placeholder).Label(cmp.Or(label, f.placeholder)).FillWidth()
			if first && !d.focused {
				in.AutoFocus()
			}
		})
	default:
		labeled(func(stacked bool) {
			in := fit(ui.TextInput(c, &f.text).Placeholder(f.placeholder).Label(f.label), stacked)
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

// runFieldAction runs what an action field does, off the main thread.
func (a *app) runFieldAction(d *dialog, f *field) {
	if f.busy || f.act == nil {
		return
	}
	f.busy, d.err = true, ""
	a.background(func() {
		apply, err := f.act(d)
		a.update(func() {
			f.busy = false
			if err != nil {
				d.err = errorText(err)
			} else if apply != nil {
				apply()
			}
			a.refresh()
		})
	})
}
