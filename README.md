# TG-Parser 🤖

[![Go Version](https://img.shields.io/badge/go-1.23+-blue.svg)](https://golang.org)

A Telegram parser written in Go that monitors messages in groups and channels, analyzes them using OpenAI, and sends notifications to the administrator about requests for developing Telegram bots or websites.

## 🚀 Features

- **Message Monitoring**: Listens for new messages in connected chats
- **AI Analysis**: Uses OpenAI GPT to identify relevant development requests
- **Notifications**: Sends beautiful notifications to the administrator with details
- **Peer Caching**: Stores user information for quick access
- **Error Handling**: Robust with logging for failures
- **Cross-Platform**: Supports ARM architecture

## 📋 Requirements

- Go 1.23+
- Telegram account with API keys
- OpenAI API key
- .env file with environment variables

## 🛠 Installation

1. Clone the repository:
   ```bash
   git clone https://github.com/your-username/tg-parser.git
   cd tg-parser
   ```

2. Install dependencies:
   ```bash
   go mod download
   ```

3. Create a `.env` file in the project root:
   ```env
   TG_PHONE=+1234567890
   APP_ID=your_app_id
   APP_HASH=your_app_hash
   OPENAI_API_KEY=your_openai_key
   ADMIN_USERNAME=@your_admin_username
   ```

## ⚙️ Configuration

1. **Get Telegram API Keys**:
   - Go to [my.telegram.org](https://my.telegram.org)
   - Create an application and get `APP_ID` and `APP_HASH`

2. **Get OpenAI API Key**:
   - Sign up at [OpenAI](https://platform.openai.com)
   - Create an API key

3. **Set up Administrator**:
   - Specify the admin username in `ADMIN_USERNAME` (with @)

## ▶️ Running

```bash
go run main.go
```

On first run, Telegram authorization will be required.

## 🔧 Building for ARM

To build for ARM architecture (e.g., Raspberry Pi):

```bash
GOOS=linux GOARCH=arm64 go build -o tg-parser-arm64 main.go
```

## 📁 Project Structure

```
tg-parser/
├── main.go           # Main application code
├── go.mod            # Go dependencies
├── go.sum            # Dependency checksums
├── .env              # Environment variables (do not commit!)
└── session/          # Directory for sessions and DB (created automatically)
```

## 🔍 How It Works

1. The bot connects to Telegram API
2. Monitors new messages in groups
3. Sends message text to OpenAI for analysis
4. If the message is relevant (development request), sends notification to admin
5. Stores user information in local database

## 📊 Notification Example

```
🔍 Development request found!

👤 @username (ID: 123456789)

💬 Looking for developer to create Telegram bot
```

## 🐛 Troubleshooting

- **Auth Error**: Check `TG_PHONE`, `APP_ID`, `APP_HASH`
- **OpenAI Errors**: Check API key and limits
- **No Notifications**: Ensure bot is added to groups with read permissions

## 🤝 Contributing

Pull requests are welcome! For major changes, please open an issue first.

## 📞 Contacts

If you have questions, write to issues or telegram: t.me/ew2df

---

⭐ If you like the project, give it a star!
