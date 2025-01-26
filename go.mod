module podcaster

go 1.21

require (
	github.com/go-telegram-bot-api/telegram-bot-api/v5 v5.5.1
	github.com/sashabaranov/go-openai v1.36.1
)

require (
	golang.org/x/net v0.34.0 // indirect
	golang.org/x/text v0.21.0 // indirect
)

// Add at the bottom of go.mod
// +heroku goVersion go1.21
// +heroku install ./...
