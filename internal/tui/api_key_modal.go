package tui

import (
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// APIKeyModal manages the API key prompt overlay.
type APIKeyModal struct {
	app        *App
	modal      *tview.Flex
	form       *tview.Form
	keyField   *tview.InputField
	onSave     func(key string)
	onCancel   func()
}

func NewAPIKeyModal(app *App) *APIKeyModal {
	am := &APIKeyModal{app: app}

	am.form = tview.NewForm()
	am.form.SetBackgroundColor(app.theme.HeaderBg)
	am.form.SetFieldBackgroundColor(app.theme.InputBg)
	am.form.SetFieldTextColor(app.theme.Foreground)
	am.form.SetButtonBackgroundColor(app.theme.Accent)
	am.form.SetButtonTextColor(app.theme.SelectionText)
	am.form.SetLabelColor(app.theme.Foreground)

	am.keyField = tview.NewInputField()
	am.keyField.SetLabel("Linear API key")
	am.keyField.SetFieldWidth(60)
	am.keyField.SetMaskCharacter('*')
	am.form.AddFormItem(am.keyField)

	am.form.AddButton("Save", func() {
		key := strings.TrimSpace(am.keyField.GetText())
		if am.onSave != nil {
			am.onSave(key)
		}
	})
	am.form.AddButton("Cancel", func() {
		if am.onCancel != nil {
			am.onCancel()
		}
	})

	headerView := tview.NewTextView()
	headerView.SetText("Set Linear API Key")
	headerView.SetTextColor(app.theme.Accent)
	headerView.SetBackgroundColor(app.theme.HeaderBg)

	helpView := tview.NewTextView()
	helpView.SetText("Enter your Linear API key to continue")
	helpView.SetTextColor(app.theme.SecondaryText)
	helpView.SetBackgroundColor(app.theme.HeaderBg)
	helpView.SetTextAlign(tview.AlignCenter)

	modalContent := tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(headerView, 1, 0, false).
		AddItem(am.form, 0, 1, true).
		AddItem(helpView, 1, 0, false)
	modalContent.Box = tview.NewBox().SetBackgroundColor(app.theme.HeaderBg)
	modalContent.SetBackgroundColor(app.theme.HeaderBg).
		SetBorder(true).
		SetBorderColor(app.theme.Accent).
		SetTitle(" API Key ").
		SetTitleColor(app.theme.Foreground)
	padding := app.density.ModalPadding
	modalContent.SetBorderPadding(padding.Top, padding.Bottom, padding.Left, padding.Right)

	am.modal = tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().
			SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(modalContent, 8, 0, true).
			AddItem(nil, 0, 1, false), 80, 0, true).
		AddItem(nil, 0, 1, false)
	am.modal.SetBackgroundColor(app.theme.Background)

	return am
}

// Show displays the API key modal and wires save/cancel handlers.
func (am *APIKeyModal) Show(onSave func(key string), onCancel func()) {
	am.onSave = onSave
	am.onCancel = onCancel
	am.keyField.SetText("")
	am.app.pages.AddPage("api_key", am.modal, true, true)
	am.app.pages.SendToFront("api_key")
	am.app.app.SetFocus(am.keyField)
}

// Hide removes the API key modal and restores focus.
func (am *APIKeyModal) Hide() {
	am.app.pages.RemovePage("api_key")
	am.app.updateFocus()
}

// HandleKey handles modal-specific key bindings.
func (am *APIKeyModal) HandleKey(event *tcell.EventKey) *tcell.EventKey {
	if event.Key() == tcell.KeyEscape {
		am.Hide()
		if am.onCancel != nil {
			am.onCancel()
		}
		return nil
	}
	return event
}
