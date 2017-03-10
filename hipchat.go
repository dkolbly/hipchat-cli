package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"

	"github.com/dkolbly/cli"
	"github.com/dkolbly/logging"
	"github.com/dkolbly/logging/pretty"
)

var log = logging.New("hipchat-cli")

func main() {

	app := &cli.App{
		Usage:   "Talk to the hipchat v2 API",
		Version: "1.0.0",
	}

	app.Commands = append(app.Commands, sendCmd)

	app.Run(os.Args)
}

var sendCmd = &cli.Command{
	Name:   "send",
	Usage:  "send a message to a room",
	Action: doSend,
	Flags: []cli.Flag{
		&cli.BoolFlag{
			Name:    "debug",
			Aliases: []string{"d"},
			Usage:   "enable debug messages",
		},
		&cli.StringFlag{
			Name:     "token",
			Aliases:  []string{"t"},
			EnvVars:  []string{"HIPCHAT_TOKEN"},
			Required: true,
			Usage:    "API token",
		},
		&cli.IntFlag{
			Name:     "room",
			Aliases:  []string{"r"},
			EnvVars:  []string{"HIPCHAT_ROOM_ID"},
			Required: true,
			Usage:    "room ID",
		},
		&cli.StringFlag{
			Name:    "from",
			Aliases: []string{"f"},
			EnvVars: []string{"HIPCHAT_FROM"},
			Usage:   "from name",
		},
		&cli.StringFlag{
			Name:    "color",
			Aliases: []string{"c"},
			EnvVars: []string{"HIPCHAT_COLOR"},
			Usage:   "message color (yellow, red, green, purple, gray or random)",
			Value:   "yellow",
		},
		&cli.StringFlag{
			Name:    "message",
			Aliases: []string{"m"},
			Usage:   "the message to send (default: from stdin)",
		},
		&cli.BoolFlag{
			Name:    "notify",
			Aliases: []string{"n"},
			Usage:   "Trigger notification for people in the room",
		},
		&cli.BoolFlag{
			Name:    "insecure",
			Aliases: []string{"k"},
			Usage:   "Don't validate SSL credentials",
		},
	},
}

func doSend(c *cli.Context) error {
	if c.Bool("debug") {
		pretty.Debug()
	}

	apiServer := "api.hipchat.com"
	roomID := c.Int("room")
	token := c.String("token")

	url := fmt.Sprintf("https://%s/v2/room/%d/notification",
		apiServer,
		roomID)

	var text string
	if c.IsSet("message") {
		text = c.String("message")
	} else {
		inp, err := ioutil.ReadAll(os.Stdin)
		if err != nil {
			log.Fatalf("Could not read message from stdin: %s", err)
		}
		text = string(inp)
	}

	msg := &Message{
		Color:         c.String("color"),
		Message:       text,
		MessageFormat: "html",
		Notify:        c.Bool("notify"),
	}
	if c.IsSet("from") {
		msg.From = c.String("from")
	}

	buf, err := json.Marshal(msg)
	if err != nil {
		log.Fatal(err)
	}

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		log.Fatal(err)
	}

	req.Header.Set("Content-Length", fmt.Sprintf("%d", len(buf)))
	req.Header.Set("Content-Type", "application/json")
	auth := fmt.Sprintf("Bearer %s", token)
	req.Header.Set("Authorization", auth)

	rsp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Fatal(err)
	}
	defer rsp.Body.Close()
	entity, err := ioutil.ReadAll(rsp.Body)
	if err != nil {
		log.Fatalf("Could not read response entity: %s", err)
	}

	switch rsp.StatusCode {
	case http.StatusOK, http.StatusNoContent:
		log.Debug("Success %s; response headers:", rsp.Status)
		for k, v := range rsp.Header {
			log.Debug("%s := %q", k, v)
		}

	default:
		log.Error("POST failed: %s\n%s", rsp.Status, entity)
		return ErrPostFailed
	}

	return nil
}

var ErrPostFailed = errors.New("posting message failed")

type Message struct {
	From          string `json:"from,omitempty"`
	Message       string `json:"message"`
	Color         string `json:"color"`
	MessageFormat string `json:"message_format"`
	Notify        bool   `json:"notify"`
}
