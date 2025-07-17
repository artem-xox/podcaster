package bot

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	openai "github.com/sashabaranov/go-openai"
)

// UserState tracks a user's current progress.
type UserState struct {
	Category   string
	Topic      string
	WaitingFor string
	ScriptText string
}

const (
	StateInitial  = "initial"
	StateCategory = "category"
	StateTopic    = "topic"
)

// Bot wraps Telegram and OpenAI clients with user state management.
type Bot struct {
	tg *tgbotapi.BotAPI
	ai *openai.Client

	mu     sync.Mutex
	states map[int64]*UserState
}

// New creates a Bot with the provided tokens.
func New(tgToken, aiKey string) (*Bot, error) {
	tg, err := tgbotapi.NewBotAPI(tgToken)
	if err != nil {
		return nil, err
	}
	ai := openai.NewClient(aiKey)

	return &Bot{
		tg:     tg,
		ai:     ai,
		states: make(map[int64]*UserState),
	}, nil
}

// Run starts listening for updates and handling them.
func (b *Bot) Run() error {
	if _, err := b.tg.Request(tgbotapi.NewSetMyCommands(
		tgbotapi.BotCommand{Command: "new", Description: "Start new podcast creation"},
		tgbotapi.BotCommand{Command: "text", Description: "Get generated podcast text"},
	)); err != nil {
		return err
	}

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := b.tg.GetUpdatesChan(u)

	for update := range updates {
		if update.Message != nil {
			b.handleMessage(update.Message)
		} else if update.CallbackQuery != nil {
			b.handleCallback(update.CallbackQuery)
		}
	}
	return nil
}

func (b *Bot) getState(userID int64) *UserState {
	b.mu.Lock()
	defer b.mu.Unlock()
	st, ok := b.states[userID]
	if !ok {
		st = &UserState{WaitingFor: StateInitial}
		b.states[userID] = st
	}
	return st
}

func (b *Bot) resetState(userID int64) {
	b.mu.Lock()
	b.states[userID] = &UserState{WaitingFor: StateInitial}
	b.mu.Unlock()
}

func (b *Bot) handleMessage(msg *tgbotapi.Message) {
	userID := msg.Chat.ID
	state := b.getState(userID)

	switch msg.Text {
	case "/new":
		b.resetState(userID)
		b.sendCategories(userID)
		return
	case "/text":
		b.handleTextRequest(userID)
		return
	}

	switch state.WaitingFor {
	case StateInitial:
		b.sendCategories(userID)
	}
}

func (b *Bot) handleCallback(query *tgbotapi.CallbackQuery) {
	userID := query.Message.Chat.ID
	data := query.Data

	state := b.getState(userID)

	switch state.WaitingFor {
	case StateCategory:
		b.handleCategorySelection(userID, data)
	case StateTopic:
		b.handleTopicSelection(userID, data)
	}

	b.tg.Send(tgbotapi.NewCallback(query.ID, ""))
}

func (b *Bot) sendCategories(userID int64) {
	categories := []string{"Auto", "Health", "Travel", "ML", "Media"}
	var buttons []tgbotapi.InlineKeyboardButton
	for _, cat := range categories {
		buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonData(cat, cat))
	}

	msg := tgbotapi.NewMessage(userID, "Choose podcast category:")
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(buttons...),
	)

	b.mu.Lock()
	b.states[userID].WaitingFor = StateCategory
	b.mu.Unlock()

	b.tg.Send(msg)
}

func (b *Bot) handleCategorySelection(userID int64, category string) {
	b.mu.Lock()
	st := b.getState(userID)
	st.Category = category
	st.WaitingFor = StateTopic
	b.mu.Unlock()

	ctx := context.Background()
	prompt := fmt.Sprintf("Generate 5 podcast topics about %s. Return as comma-separated list.", category)
	resp, err := b.ai.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:    openai.GPT4o,
		Messages: []openai.ChatCompletionMessage{{Role: openai.ChatMessageRoleUser, Content: prompt}},
	})
	if err != nil {
		b.sendError(userID)
		return
	}

	topics := splitTopics(resp.Choices[0].Message.Content)
	b.sendTopics(userID, topics)
}

func (b *Bot) sendTopics(userID int64, topics []string) {
	var buttons []tgbotapi.InlineKeyboardButton
	for _, topic := range topics {
		buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonData(topic, topic))
	}

	msg := tgbotapi.NewMessage(userID, "Choose a specific topic:")
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(buttons[:3]...),
		tgbotapi.NewInlineKeyboardRow(buttons[3:]...),
	)

	b.tg.Send(msg)
}

func (b *Bot) handleTopicSelection(userID int64, topic string) {
	st := b.getState(userID)
	b.mu.Lock()
	st.Topic = topic
	b.mu.Unlock()

	ctx := context.Background()
	prompt := fmt.Sprintf("Create a 2-minute podcast script about %s in %s category. Keep it under 400 words.", topic, st.Category)
	resp, err := b.ai.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:    openai.GPT4o,
		Messages: []openai.ChatCompletionMessage{{Role: openai.ChatMessageRoleUser, Content: prompt}},
	})
	if err != nil {
		b.sendError(userID)
		return
	}

	script := resp.Choices[0].Message.Content
	b.mu.Lock()
	st.ScriptText = script
	b.mu.Unlock()

	b.generateAndSendAudio(userID, script)
}

func (b *Bot) handleTextRequest(userID int64) {
	st := b.getState(userID)

	if st.ScriptText == "" {
		msg := tgbotapi.NewMessage(userID, "No script available. Please create a podcast first!")
		b.tg.Send(msg)
		return
	}

	script := st.ScriptText
	maxLength := 4000
	if len(script) > maxLength {
		script = script[:maxLength] + "\n... [truncated]"
	}

	msg := tgbotapi.NewMessage(userID, script)
	msg.ParseMode = tgbotapi.ModeMarkdown
	b.tg.Send(msg)
}

func (b *Bot) generateAndSendAudio(userID int64, text string) {
	ctx := context.Background()
	req := openai.CreateSpeechRequest{
		Model: openai.TTSModel1,
		Input: text,
		Voice: openai.VoiceAlloy,
	}

	resp, err := b.ai.CreateSpeech(ctx, req)
	if err != nil {
		b.sendError(userID)
		return
	}
	defer resp.Close()

	audioData, err := io.ReadAll(resp)
	if err != nil {
		b.sendError(userID)
		return
	}

	audioPath := fmt.Sprintf("%d.mp3", userID)
	if err := os.WriteFile(audioPath, audioData, 0644); err != nil {
		b.sendError(userID)
		return
	}
	defer os.Remove(audioPath)

	audioMsg := tgbotapi.NewAudio(userID, tgbotapi.FilePath(audioPath))
	audioMsg.Caption = "Here's your podcast, enjoy!"
	b.tg.Send(audioMsg)
}

func splitTopics(input string) []string {
	return strings.Split(input, ",")
}

func (b *Bot) sendError(userID int64) {
	msg := tgbotapi.NewMessage(userID, "Error generating content. Please try again.")
	b.tg.Send(msg)
	b.sendCategories(userID)
}
