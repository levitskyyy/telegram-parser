package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"time"

	pebbledb "github.com/cockroachdb/pebble"
	"github.com/go-faster/errors"
	boltstor "github.com/gotd/contrib/bbolt"
	"github.com/gotd/contrib/middleware/floodwait"
	"github.com/gotd/contrib/middleware/ratelimit"
	"github.com/gotd/contrib/pebble"
	"github.com/gotd/contrib/storage"
	"github.com/gotd/td/examples"
	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/message"
	"github.com/gotd/td/telegram/query"
	"github.com/gotd/td/telegram/query/dialogs"
	"github.com/gotd/td/telegram/updates"
	"github.com/gotd/td/tg"
	"go.etcd.io/bbolt"
	"golang.org/x/time/rate"
	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/joho/godotenv"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	openai "github.com/sashabaranov/go-openai"
)

func sessionFolder(phone string) string {
	var out []rune
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			out = append(out, r)
		}
	}
	return "phone-" + string(out)
}

func getChatID(peer tg.PeerClass) int64 {
	switch p := peer.(type) {
	case *tg.PeerUser:
		return p.UserID
	case *tg.PeerChat:
		return p.ChatID
	case *tg.PeerChannel:
		return p.ChannelID
	default:
		return 0
	}
}

func getPeerKind(peer tg.PeerClass) dialogs.PeerKind {
	switch peer.(type) {
	case *tg.PeerUser:
		return dialogs.User
	case *tg.PeerChat:
		return dialogs.Chat
	case *tg.PeerChannel:
		return dialogs.Channel
	default:
		return dialogs.User
	}
}

func resolveAdminPeer(ctx context.Context, api *tg.Client, username string) (tg.InputPeerClass, error) {
	resp, err := api.ContactsResolveUsername(ctx, &tg.ContactsResolveUsernameRequest{
		Username: trimAt(username),
	})
	if err != nil {
		return nil, errors.Wrap(err, "resolve username")
	}
	for _, u := range resp.Users {
		if user, ok := u.(*tg.User); ok {
			return &tg.InputPeerUser{UserID: user.ID, AccessHash: user.AccessHash}, nil
		}
	}
	return nil, errors.New("admin user not found")
}

func trimAt(s string) string {
	if len(s) > 0 && s[0] == '@' {
		return s[1:]
	}
	return s
}

func isDevelopmentRelated(ctx context.Context, client *openai.Client, text string) (bool, error) {
	prompt := fmt.Sprintf(
		`Определи, указывает ли следующее сообщение на потребность в разработке Telegram-бота или сайта. Верни только "true" или "false".
Примеры релевантных:
- "Ищу разработчика для создания Telegram-бота для группы"
- "Нужен сайт для бизнеса, есть разработчики?"
- "Кто может сделать бота для автоматизации в Telegram?"
Нерелевантные:
- "Привет, как дела?"
- "Кто хочет встретиться за кофе?"

Сообщение: %s`, text)

	resp, err := client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: "gpt-4o-mini",
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleSystem, Content: prompt},
		},
		MaxTokens:   5,
		Temperature: 0,
	})
	if err != nil {
		return false, err
	}
	if len(resp.Choices) == 0 {
		return false, errors.New("openai: empty response")
	}
	return resp.Choices[0].Message.Content == "true", nil
}

func main() {
	if err := godotenv.Load(); err != nil {
		fmt.Printf("Error loading .env file: %v\n", err)
		os.Exit(1)
	}

	phone := os.Getenv("TG_PHONE")
	if phone == "" {
		fmt.Println("TG_PHONE is required (e.g. +123456789)")
		os.Exit(1)
	}
	appID, err := strconv.Atoi(os.Getenv("APP_ID"))
	if err != nil || appID == 0 {
		fmt.Println("APP_ID is required (int)")
		os.Exit(1)
	}
	appHash := os.Getenv("APP_HASH")
	if appHash == "" {
		fmt.Println("APP_HASH is required")
		os.Exit(1)
	}
	openAIKey := os.Getenv("OPENAI_API_KEY")
	if openAIKey == "" {
		fmt.Println("OPENAI_API_KEY is required")
		os.Exit(1)
	}
	adminUsername := os.Getenv("ADMIN_USERNAME")
	if adminUsername == "" {
		fmt.Println("ADMIN_USERNAME is required (e.g. @ew2df)")
		os.Exit(1)
	}

	openaiClient := openai.NewClient(openAIKey)

	// ---- Session + logs ----
	sessionDir := filepath.Join("session", sessionFolder(phone))
	if err := os.MkdirAll(sessionDir, 0o700); err != nil {
		fmt.Printf("mkdir session: %v\n", err)
		os.Exit(1)
	}
	logFilePath := filepath.Join(sessionDir, "log.jsonl")

	logWriter := zapcore.AddSync(&lumberjack.Logger{
		Filename:   logFilePath,
		MaxBackups: 3,
		MaxSize:    2, // MB
		MaxAge:     7, // days
	})
	logCore := zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		logWriter,
		zap.DebugLevel,
	)
	lg := zap.New(logCore)
	defer func() { _ = lg.Sync() }()

	sessionStorage := &telegram.FileSessionStorage{
		Path: filepath.Join(sessionDir, "session.json"),
	}

	// ---- Peer storage & updates state ----
	db, err := pebbledb.Open(filepath.Join(sessionDir, "peers.pebble.db"), &pebbledb.Options{})
	if err != nil {
		fmt.Printf("pebble open: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()
	peerDB := pebble.NewPeerStorage(db)

	boltdb, err := bbolt.Open(filepath.Join(sessionDir, "updates.bolt.db"), 0o666, nil)
	if err != nil {
		fmt.Printf("bolt open: %v\n", err)
		os.Exit(1)
	}
	defer boltdb.Close()

	dispatcher := tg.NewUpdateDispatcher()
	updateHandler := storage.UpdateHook(dispatcher, peerDB)
	updatesRecovery := updates.New(updates.Config{
		Handler: updateHandler,
		Logger:  lg.Named("updates.recovery"),
		Storage: boltstor.NewStateStorage(boltdb),
	})

	// FLOOD_WAIT & rate limit middlewares
	waiter := floodwait.NewWaiter().WithCallback(func(ctx context.Context, wait floodwait.FloodWait) {
		lg.Warn("Flood wait", zap.Duration("wait", wait.Duration))
		fmt.Println("FLOOD_WAIT, retry after:", wait.Duration)
	})

	client := telegram.NewClient(appID, appHash, telegram.Options{
		Logger:         lg,
		SessionStorage: sessionStorage,
		UpdateHandler:  updatesRecovery,
		Middlewares: []telegram.Middleware{
			waiter,
			ratelimit.New(rate.Every(100*time.Millisecond), 5),
		},
	})
	api := client.API()

	// ---- Sender for admin ----
	sender := message.NewSender(api)

	// ---- OnNewMessage handler ----
	dispatcher.OnNewMessage(func(ctx context.Context, e tg.Entities, u *tg.UpdateNewMessage) error {
		msg, ok := u.Message.(*tg.Message)
		if !ok || msg == nil || msg.Message == "" {
			return nil
		}
		if msg.Out {
			return nil
		}

		p, err := storage.FindPeer(ctx, peerDB, msg.GetPeerID())
		if err != nil {
			p = storage.Peer{
				Version: storage.LatestVersion,
				Key: dialogs.DialogKey{
					ID:   getChatID(msg.GetPeerID()),
					Kind: getPeerKind(msg.GetPeerID()),
				},
				CreatedAt: time.Now(),
			}
		}

		isDev, err := isDevelopmentRelated(ctx, openaiClient, msg.Message)
		if err != nil {
			fmt.Printf("OpenAI error: %v\n", err)
			return nil
		}
		if !isDev {
			return nil
		}

		adminPeer, err := resolveAdminPeer(ctx, api, adminUsername)
		if err != nil {
			fmt.Printf("resolve admin: %v\n", err)
			return nil
		}

		fromID := int64(0)
		if fu, ok := msg.FromID.(*tg.PeerUser); ok {
			fromID = fu.UserID
		}

		username := "unknown"
		if p.User != nil && p.User.Username != "" {
			username = "@" + p.User.Username
		}

		summary := fmt.Sprintf(
			"🔍 Найден запрос на разработку!\n\n👤 %s (ID: %d)\n\n💬 %s",
			username, fromID, msg.Message,
		)

		if _, err := sender.To(adminPeer).Text(ctx, summary); err != nil {
			fmt.Printf("send to admin: %v\n", err)
		} else {
			fmt.Printf("Forwarded to %s: %s\n", adminUsername, summary)
		}
		return nil
	})

	// ---- Run with auth & updates recovery ----
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	flow := auth.NewFlow(examples.Terminal{PhoneNumber: phone}, auth.SendCodeOptions{})

	if err := waiter.Run(ctx, func(ctx context.Context) error {
		return client.Run(ctx, func(ctx context.Context) error {
			if err := client.Auth().IfNecessary(ctx, flow); err != nil {
				return errors.Wrap(err, "auth")
			}

			self, err := client.Self(ctx)
			if err != nil {
				return errors.Wrap(err, "self")
			}
			fmt.Printf("Logged in as %s (id=%d, @%s)\n", self.FirstName, self.ID, self.Username)

			collector := storage.CollectPeers(peerDB)
			if err := collector.Dialogs(ctx, query.GetDialogs(api).Iter()); err != nil {
				fmt.Printf("collect peers: %v\n", err)
			}

			fmt.Println("Listening for updates...")
			return updatesRecovery.Run(ctx, api, self.ID, updates.AuthOptions{
				IsBot: self.Bot,
				OnStart: func(ctx context.Context) {
					fmt.Println("Update recovery started")
				},
			})
		})
	}); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %+v\n", err)
		os.Exit(1)
	}
}
