package main

import (
	"log"
	"os"

	"podcaster/internal/bot"
)

func main() {
	tgToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	aiKey := os.Getenv("OPENAI_API_KEY")

	b, err := bot.New(tgToken, aiKey)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("bot is starting...")
	if err := b.Run(); err != nil {
		log.Fatal(err)
	}
}
