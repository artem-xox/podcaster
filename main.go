package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	openai "github.com/sashabaranov/go-openai"
)

type UserState struct {
	Category   string
	Topic      string
	WaitingFor string
	ScriptText string // Add this field
}

var (
	bot         *tgbotapi.BotAPI
	aiClient    *openai.Client
	userStates  = make(map[int64]*UserState)
	statesMutex = &sync.Mutex{}
)

const (
	StateInitial  = "initial"
	StateCategory = "category"
	StateTopic    = "topic"
)

func main() {
	// Initialize Telegram bot
	var err error
	bot, err = tgbotapi.NewBotAPI(os.Getenv("TELEGRAM_BOT_TOKEN"))
	if err != nil {
		log.Panic(err)
	}

	_, err = bot.Request(tgbotapi.NewSetMyCommands(
		tgbotapi.BotCommand{
			Command:     "new",
			Description: "Start new podcast creation",
		},
		tgbotapi.BotCommand{
			Command:     "text",
			Description: "Get generated podcast text",
		},
	))
	if err != nil {
		log.Printf("Error setting commands: %v", err)
	}

	// Initialize OpenAI client
	aiClient = openai.NewClient(os.Getenv("OPENAI_API_KEY"))

	// Configure updates
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	updates := bot.GetUpdatesChan(u)

	// Handle incoming updates
	for update := range updates {
		if update.Message != nil {
			handleMessage(update.Message)
		} else if update.CallbackQuery != nil {
			handleCallback(update.CallbackQuery)
		}
	}
}

func handleMessage(msg *tgbotapi.Message) {
	userID := msg.Chat.ID

	// Initialize user state if not exists
	statesMutex.Lock()
	if userStates[userID] == nil {
		userStates[userID] = &UserState{WaitingFor: StateInitial}
	}
	state := userStates[userID]
	statesMutex.Unlock()

	// Handle /new command
	switch msg.Text {
	case "/new":
		statesMutex.Lock()
		userStates[userID] = &UserState{WaitingFor: StateInitial}
		statesMutex.Unlock()
		sendCategories(userID)
		return
	case "/text":
		handleTextRequest(userID)
		return
	}

	switch state.WaitingFor {
	case StateInitial:
		sendCategories(userID)
	}
}

func handleCallback(query *tgbotapi.CallbackQuery) {
	userID := query.Message.Chat.ID
	data := query.Data

	statesMutex.Lock()
	state := userStates[userID]
	statesMutex.Unlock()

	switch state.WaitingFor {
	case StateCategory:
		handleCategorySelection(userID, data)
	case StateTopic:
		handleTopicSelection(userID, data)
	}

	bot.Send(tgbotapi.NewCallback(query.ID, ""))
}

func sendCategories(userID int64) {
	categories := []string{"Auto", "Health", "Travel", "ML", "Media"}
	var buttons []tgbotapi.InlineKeyboardButton

	for _, cat := range categories {
		buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonData(cat, cat))
	}

	msg := tgbotapi.NewMessage(userID, "Choose podcast category:")
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(buttons...),
	)

	statesMutex.Lock()
	userStates[userID].WaitingFor = StateCategory
	statesMutex.Unlock()

	bot.Send(msg)
}

func handleCategorySelection(userID int64, category string) {
	statesMutex.Lock()
	userStates[userID].Category = category
	userStates[userID].WaitingFor = StateTopic
	statesMutex.Unlock()

	// Generate topics using OpenAI
	ctx := context.Background()
	prompt := fmt.Sprintf("Generate 5 podcast topics about %s. Return as comma-separated list.", category)

	resp, err := aiClient.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: openai.GPT4o,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleUser, Content: prompt},
		},
	})

	if err != nil {
		sendError(userID)
		return
	}

	fmt.Println(resp.Choices[0].Message.Content)

	topics := splitTopics(resp.Choices[0].Message.Content)

	sendTopics(userID, topics)
}

func sendTopics(userID int64, topics []string) {
	var buttons []tgbotapi.InlineKeyboardButton
	for _, topic := range topics {
		buttons = append(buttons, tgbotapi.NewInlineKeyboardButtonData(topic, topic))
	}

	msg := tgbotapi.NewMessage(userID, "Choose a specific topic:")
	msg.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(buttons[:3]...),
		tgbotapi.NewInlineKeyboardRow(buttons[3:]...),
	)

	bot.Send(msg)
}

func handleTopicSelection(userID int64, topic string) {
	statesMutex.Lock()
	state := userStates[userID]
	state.Topic = topic
	statesMutex.Unlock()

	// Generate summary
	ctx := context.Background()
	prompt := fmt.Sprintf("Create a 2-minute podcast script about %s in %s category. Keep it under 400 words.", topic, state.Category)

	resp, err := aiClient.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: openai.GPT4o,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleUser, Content: prompt},
		},
	})

	if err != nil {
		sendError(userID)
		return
	}

	script := resp.Choices[0].Message.Content

	// Store the script in user state
	statesMutex.Lock()
	userStates[userID].ScriptText = script
	statesMutex.Unlock()

	generateAndSendAudio(userID, script)
}

func handleTextRequest(userID int64) {
	statesMutex.Lock()
	state := userStates[userID]
	script := state.ScriptText
	statesMutex.Unlock()

	if script == "" {
		msg := tgbotapi.NewMessage(userID, "No script available. Please create a podcast first!")
		bot.Send(msg)
		return
	}

	// Split long text to avoid Telegram message limits (4096 characters)
	maxLength := 4000
	if len(script) > maxLength {
		script = script[:maxLength] + "\n... [truncated]"
	}

	msg := tgbotapi.NewMessage(userID, script)
	msg.ParseMode = tgbotapi.ModeMarkdown
	bot.Send(msg)
}

func generateAndSendAudio(userID int64, text string) {
	ctx := context.Background()
	req := openai.CreateSpeechRequest{
		Model: openai.TTSModel1,
		Input: text,
		Voice: openai.VoiceAlloy,
	}

	resp, err := aiClient.CreateSpeech(ctx, req)
	if err != nil {
		sendError(userID)
		return
	}
	defer resp.Close()

	// Read audio data from response
	audioData, err := io.ReadAll(resp)
	if err != nil {
		sendError(userID)
		return
	}

	// Save and send audio
	audioPath := fmt.Sprintf("%d.mp3", userID)
	if err := os.WriteFile(audioPath, audioData, 0644); err != nil {
		sendError(userID)
		return
	}
	defer os.Remove(audioPath)

	audioMsg := tgbotapi.NewAudio(userID, tgbotapi.FilePath(audioPath))
	audioMsg.Caption = "Here's your podcast, enjoy!"
	bot.Send(audioMsg)
}

// Helper functions
func splitTopics(input string) []string {
	// Implement parsing logic based on OpenAI response format
	return strings.Split(input, ",")
}

func sendError(userID int64) {
	msg := tgbotapi.NewMessage(userID, "Error generating content. Please try again.")
	bot.Send(msg)
	sendCategories(userID)
}
