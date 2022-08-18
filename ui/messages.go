package ui

import (
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/atotto/clipboard"
	"github.com/diamondburned/arikawa/v3/api"
	dsc "github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/utils/json/option"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/skratchdot/open-golang/open"
)

var linkRegex = regexp.MustCompile("https?://.+")

type MessagesTextView struct {
	*tview.TextView
	app *App
}

func NewMessagesTextView(app *App) *MessagesTextView {
	mtv := &MessagesTextView{
		TextView: tview.NewTextView(),
		app:      app,
	}

	mtv.SetDynamicColors(true)
	mtv.SetRegions(true)
	mtv.SetWordWrap(true)
	mtv.SetInputCapture(mtv.onInputCapture)
	mtv.SetChangedFunc(func() {
		mtv.app.Draw()
	})

	mtv.SetTitleAlign(tview.AlignLeft)
	mtv.SetBorder(true)
	mtv.SetBorderPadding(0, 0, 1, 1)

	return mtv
}

func (mtv *MessagesTextView) onInputCapture(e *tcell.EventKey) *tcell.EventKey {
	if mtv.app.SelectedChannel == nil {
		return nil
	}

	// Messages should return messages ordered from latest to earliest.
	ms, err := mtv.app.State.Cabinet.Messages(mtv.app.SelectedChannel.ID)
	if err != nil || len(ms) == 0 {
		return nil
	}

	switch e.Name() {
	case mtv.app.Config.Keys.SelectPreviousMessage:
		// If there are no highlighted regions, select the latest (last) message in the messages TextView.
		if len(mtv.app.MessagesTextView.GetHighlights()) == 0 {
			mtv.app.SelectedMessage = 0
		} else {
			// If the selected message is the oldest (first) message, select the latest (last) message in the messages TextView.
			if mtv.app.SelectedMessage == len(ms)-1 {
				mtv.app.SelectedMessage = 0
			} else {
				mtv.app.SelectedMessage++
			}
		}

		mtv.app.MessagesTextView.
			Highlight(ms[mtv.app.SelectedMessage].ID.String()).
			ScrollToHighlight()
		return nil
	case mtv.app.Config.Keys.SelectNextMessage:
		// If there are no highlighted regions, select the latest (last) message in the messages TextView.
		if len(mtv.app.MessagesTextView.GetHighlights()) == 0 {
			mtv.app.SelectedMessage = 0
		} else {
			// If the selected message is the latest (last) message, select the oldest (first) message in the messages TextView.
			if mtv.app.SelectedMessage == 0 {
				mtv.app.SelectedMessage = len(ms) - 1
			} else {
				mtv.app.SelectedMessage--
			}
		}

		mtv.app.MessagesTextView.
			Highlight(ms[mtv.app.SelectedMessage].ID.String()).
			ScrollToHighlight()
		return nil
	case mtv.app.Config.Keys.SelectFirstMessage:
		mtv.app.SelectedMessage = len(ms) - 1
		mtv.app.MessagesTextView.
			Highlight(ms[mtv.app.SelectedMessage].ID.String()).
			ScrollToHighlight()
		return nil
	case mtv.app.Config.Keys.SelectLastMessage:
		mtv.app.SelectedMessage = 0
		mtv.app.MessagesTextView.
			Highlight(ms[mtv.app.SelectedMessage].ID.String()).
			ScrollToHighlight()
		return nil
	case mtv.app.Config.Keys.OpenMessageActionsList:
		hs := mtv.app.MessagesTextView.GetHighlights()
		if len(hs) == 0 {
			return nil
		}

		mID, err := dsc.ParseSnowflake(hs[0])
		if err != nil {
			return nil
		}

		_, m := findMessageByID(ms, dsc.MessageID(mID))
		if m == nil {
			return nil
		}

		actionsList := tview.NewList()
		actionsList.ShowSecondaryText(false)
		actionsList.SetDoneFunc(func() {
			mtv.app.
				SetRoot(mtv.app.MainFlex, true).
				SetFocus(mtv.app.MessagesTextView)
		})
		actionsList.SetTitle("Press the Escape key to close")
		actionsList.SetTitleAlign(tview.AlignLeft)
		actionsList.SetBorder(true)
		actionsList.SetBorderPadding(0, 0, 1, 1)

		// If the client user has `SEND_MESSAGES` permission, add a new action to reply to the message.
		if hasPermission(mtv.app.State, mtv.app.SelectedChannel.ID, dsc.PermissionSendMessages) {
			actionsList.AddItem("Reply", "", 'r', func() {
				mtv.app.MessageInputField.SetTitle("Replying to " + m.Author.Tag())
				mtv.app.
					SetRoot(mtv.app.MainFlex, true).
					SetFocus(mtv.app.MessageInputField)
			})

			actionsList.AddItem("Mention Reply", "", 'R', func() {
				mtv.app.MessageInputField.SetTitle("[@] Replying to " + m.Author.Tag())
				mtv.app.
					SetRoot(mtv.app.MainFlex, true).
					SetFocus(mtv.app.MessageInputField)
			})
		}

		// If the client user has the `MANAGE_MESSAGES` permission, add a new action to delete the message.
		if hasPermission(mtv.app.State, mtv.app.SelectedChannel.ID, dsc.PermissionManageMessages) {
			actionsList.AddItem("Delete", "", 'd', func() {
				go mtv.deleteMessage(*m)
				mtv.app.
					SetRoot(mtv.app.MainFlex, true).
					SetFocus(mtv.app.MessagesTextView)
			})
		}

		// If the referenced message exists, add a new action to select the reply.
		if m.ReferencedMessage != nil {
			actionsList.AddItem("Select Reply", "", 'm', func() {
				mtv.app.SelectedMessage, _ = findMessageByID(ms, m.ReferencedMessage.ID)
				mtv.app.MessagesTextView.
					Highlight(m.ReferencedMessage.ID.String()).
					ScrollToHighlight()
				mtv.app.
					SetRoot(mtv.app.MainFlex, true).
					SetFocus(mtv.app.MessagesTextView)
			})
		}

		// If the content of the message contains link(s), add the appropriate actions to the list.
		links := linkRegex.FindAllString(m.Content, -1)
		if len(links) != 0 {
			actionsList.AddItem("Open Link", "", 'l', func() {
				for _, l := range links {
					go open.Run(l)
				}
			})
		}

		// If the message contains attachments, add the appropriate actions to the actions list.
		if len(m.Attachments) != 0 {
			actionsList.AddItem("Download Attachment", "", 'd', func() {
				go mtv.downloadAttachment(m.Attachments)
				mtv.app.SetRoot(mtv.app.MainFlex, true)
			})
			actionsList.AddItem("Open Attachment", "", 'o', func() {
				go mtv.openAttachment(m.Attachments)
				mtv.app.SetRoot(mtv.app.MainFlex, true)
			})
		}

		actionsList.AddItem("Copy Content", "", 'c', func() {
			if err := clipboard.WriteAll(m.Content); err != nil {
				return
			}

			mtv.app.SetRoot(mtv.app.MainFlex, true)
			mtv.app.SetFocus(mtv.app.MessagesTextView)
		})
		actionsList.AddItem("Copy ID", "", 'i', func() {
			if err := clipboard.WriteAll(m.ID.String()); err != nil {
				return
			}

			mtv.app.SetRoot(mtv.app.MainFlex, true)
			mtv.app.SetFocus(mtv.app.MessagesTextView)
		})

		mtv.app.SetRoot(actionsList, true)
		return nil
	case "Esc":
		mtv.app.SelectedMessage = -1
		mtv.app.SetFocus(mtv.app.MainFlex)
		mtv.app.MessagesTextView.
			Clear().
			Highlight()
		return nil
	}

	return e
}

func (mtv *MessagesTextView) downloadAttachment(as []dsc.Attachment) error {
	for _, a := range as {
		f, err := os.Create(filepath.Join(mtv.app.Config.AttachmentDownloadsDir, a.Filename))
		if err != nil {
			return err
		}
		defer f.Close()

		resp, err := http.Get(a.URL)
		if err != nil {
			return err
		}

		d, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		f.Write(d)
	}

	return nil
}

func (mtv *MessagesTextView) openAttachment(as []dsc.Attachment) error {
	for _, a := range as {
		cacheDirPath, _ := os.UserCacheDir()
		f, err := os.Create(filepath.Join(cacheDirPath, a.Filename))
		if err != nil {
			return err
		}
		defer f.Close()

		resp, err := http.Get(a.URL)
		if err != nil {
			return err
		}

		d, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		f.Write(d)
		go open.Run(f.Name())
	}

	return nil
}

func (mtv *MessagesTextView) deleteMessage(m dsc.Message) {
	mtv.Clear()

	err := mtv.app.State.MessageRemove(m.ChannelID, m.ID)
	if err != nil {
		return
	}

	err = mtv.app.State.DeleteMessage(m.ChannelID, m.ID, "Unknown")
	if err != nil {
		return
	}

	// The returned slice will be sorted from latest to oldest.
	ms, err := mtv.app.State.Messages(m.ChannelID, mtv.app.Config.MessagesLimit)
	if err != nil {
		return
	}

	for i := len(ms) - 1; i >= 0; i-- {
		_, err = mtv.app.MessagesTextView.Write(buildMessage(mtv.app, ms[i]))
		if err != nil {
			return
		}
	}
}

type MessageInput struct {
	*tview.InputField
	app *App
}

func NewMessageInput(app *App) *MessageInput {
	mi := &MessageInput{
		InputField: tview.NewInputField(),
		app:        app,
	}

	mi.SetFieldBackgroundColor(tview.Styles.PrimitiveBackgroundColor)
	mi.SetPlaceholder("Message...")
	mi.SetPlaceholderStyle(tcell.StyleDefault.Background(tview.Styles.PrimitiveBackgroundColor))
	mi.SetTitleAlign(tview.AlignLeft)
	mi.SetBorder(true)
	mi.SetBorderPadding(0, 0, 1, 1)
	mi.SetInputCapture(mi.onInputCapture)
	return mi
}

func (mi *MessageInput) onInputCapture(e *tcell.EventKey) *tcell.EventKey {
	switch e.Name() {
	case "Enter":
		if mi.app.SelectedChannel == nil {
			return nil
		}

		t := strings.TrimSpace(mi.app.MessageInputField.GetText())
		if t == "" {
			return nil
		}

		ms, err := mi.app.State.Messages(mi.app.SelectedChannel.ID, mi.app.Config.MessagesLimit)
		if err != nil {
			return nil
		}

		if len(mi.app.MessagesTextView.GetHighlights()) != 0 {
			mID, err := dsc.ParseSnowflake(mi.app.MessagesTextView.GetHighlights()[0])
			if err != nil {
				return nil
			}

			_, m := findMessageByID(ms, dsc.MessageID(mID))
			d := api.SendMessageData{
				Content:         t,
				Reference:       m.Reference,
				AllowedMentions: &api.AllowedMentions{RepliedUser: option.False},
			}

			// If the title of the message InputField widget has "[@]" as a prefix, send the message as a reply and mention the replied user.
			if strings.HasPrefix(mi.app.MessageInputField.GetTitle(), "[@]") {
				d.AllowedMentions.RepliedUser = option.True
			}

			go mi.app.State.SendMessageComplex(m.ChannelID, d)

			mi.app.SelectedMessage = -1
			mi.app.MessagesTextView.Highlight()

			mi.app.MessageInputField.SetTitle("")
		} else {
			go mi.app.State.SendMessage(mi.app.SelectedChannel.ID, t)
		}

		mi.app.MessageInputField.SetText("")

		return nil
	case "Ctrl+V":
		text, _ := clipboard.ReadAll()
		text = mi.app.MessageInputField.GetText() + text
		mi.app.MessageInputField.SetText(text)

		return nil
	case "Esc":
		mi.app.MessageInputField.
			SetText("").
			SetTitle("")
		mi.app.SetFocus(mi.app.MainFlex)

		mi.app.SelectedMessage = -1
		mi.app.MessagesTextView.Highlight()

		return nil
	case mi.app.Config.Keys.OpenExternalEditor:
		e := os.Getenv("EDITOR")
		if e == "" {
			return nil
		}

		f, err := os.CreateTemp(os.TempDir(), "discordo-*.md")
		if err != nil {
			return nil
		}
		defer os.Remove(f.Name())

		cmd := exec.Command(e, f.Name())
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout

		mi.app.Suspend(func() {
			err = cmd.Run()
			if err != nil {
				return
			}
		})

		b, err := io.ReadAll(f)
		if err != nil {
			return nil
		}

		mi.app.MessageInputField.SetText(string(b))

		return nil
	}

	return e
}
