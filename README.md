# Podcaster

Podcaster is a small Telegram bot that helps you quickly generate short podcasts. It uses OpenAI to create a script based on a category and topic, then turns that script into an audio message with OpenAI's text‑to‑speech API.

## Features

- Start a new podcast with the `/new` command.
- Select from categories such as **Auto**, **Health**, **Travel**, **ML**, and **Media**.
- Receive several suggested topics for your chosen category.
- Generate a short script and corresponding audio file.
- Use `/text` to retrieve the generated script in text form.

## Prerequisites

- Go 1.21 or newer
- A Telegram bot token (`TELEGRAM_BOT_TOKEN`)
- An OpenAI API key (`OPENAI_API_KEY`)

## Getting Started

1. Clone this repository and change into the project directory.

   ```bash
   git clone <repo-url>
   cd podcaster
   ```
2. Export the required environment variables:

   ```bash
   export TELEGRAM_BOT_TOKEN=your-bot-token
   export OPENAI_API_KEY=your-openai-key
   ```
3. Run the bot locally:

   ```bash
   go run .
   ```

   You can also build a binary with `go build` and run the resulting `podcaster` executable.

The included `Procfile` (`worker: podcaster`) shows a minimal setup for hosting on platforms such as Heroku.

## Documentation

A simple architecture diagram is provided in `docs/podcaster-design.jpg`.

## License

This project is licensed under the [Apache 2.0](LICENSE) license.
