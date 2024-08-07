package main

import (
	"context"
	"log"
	"os"
	"os/signal"

	"github.com/diamondburned/arikawa/v3/api"
	"github.com/diamondburned/arikawa/v3/api/cmdroute"
	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/diamondburned/arikawa/v3/gateway"
	"github.com/diamondburned/arikawa/v3/state"
	"github.com/diamondburned/arikawa/v3/utils/json/option"
	openai "github.com/sashabaranov/go-openai"
)

var commands = []api.CreateCommandData{
	{
		Name:        "gpt",
		Description: "Chat with OpenAI GPT-4o Mini",
		Options: []discord.CommandOption{
			&discord.StringOption{
				OptionName:  "prompt",
				Description: "What do you need help with today?",
				Required:    true,
			},
		},
	},
}

func main() {
	token := os.Getenv("BOT_TOKEN")
	if token == "" {
		log.Fatalln("No $BOT_TOKEN given.")
	}

	h := newHandler(state.New("Bot " + token))
	h.s.AddInteractionHandler(h)
	h.s.AddIntents(gateway.IntentGuilds)
	h.s.AddHandler(func(*gateway.ReadyEvent) {
		me, _ := h.s.Me()
		log.Println("connected to the gateway as", me.Tag())
	})

	if err := cmdroute.OverwriteCommands(h.s, commands); err != nil {
		log.Fatalln("cannot update commands:", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if err := h.s.Connect(ctx); err != nil {
		log.Fatalln("cannot connect:", err)
	}
}

type handler struct {
	*cmdroute.Router
	s *state.State
}

func newHandler(s *state.State) *handler {
	h := &handler{s: s}

	h.Router = cmdroute.NewRouter()
	h.Use(cmdroute.Deferrable(s, cmdroute.DeferOpts{}))
	h.AddFunc("gpt", h.cmdGpt)

	return h
}

func (h *handler) cmdGpt(ctx context.Context, data cmdroute.CommandData) *api.InteractionResponseData {
	var options struct {
		Prompt string `discord:"prompt"`
	}

	if err := data.Options.Unmarshal(&options); err != nil {
		return errorResponse(err)
	}

	channel, err := h.s.Channel(data.Event.ChannelID)
	if err != nil {
		return errorResponse(err)
	}

	// Get the last 5 messages in the channel as context
	messages, err := h.s.Messages(channel.ID, 5)
	if err != nil {
		return errorResponse(err)
	}

	// Reverse the order of messages to get the correct chronological order
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	var context []openai.ChatCompletionMessage
	for _, message := range messages {
		role := openai.ChatMessageRoleUser
		if message.Author.Bot {
			role = openai.ChatMessageRoleAssistant
		}
		context = append(context, openai.ChatCompletionMessage{
			Role:    role,
			Content: message.Content,
		})
	}

	context = append(context, openai.ChatCompletionMessage{
		Role:    openai.ChatMessageRoleUser,
		Content: options.Prompt,
	})

	result, err := getChatGptCompletion(context)
	if err != nil {
		return errorResponse(err)
	}

	response := result
	return &api.InteractionResponseData{
		Content: option.NewNullableString(response),
	}
}

func errorResponse(err error) *api.InteractionResponseData {
	return &api.InteractionResponseData{
		Content:         option.NewNullableString("**Error:** " + err.Error()),
		Flags:           discord.EphemeralMessage,
		AllowedMentions: &api.AllowedMentions{},
	}
}

func getChatGptCompletion(messages []openai.ChatCompletionMessage) (string, error) {
	client := openai.NewClient(os.Getenv("OPENAI_API_KEY"))
	resp, err := client.CreateChatCompletion(
		context.Background(),
		openai.ChatCompletionRequest{
			Model:     openai.GPT4oMini,
			Messages:  messages,
			MaxTokens: 400,
		},
	)

	if err != nil {
		return "", err
	}

	return resp.Choices[0].Message.Content, nil
}
