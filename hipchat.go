package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io/ioutil"
	"net/http"
	"os"
	"regexp"

	"github.com/dkolbly/cli"
	"github.com/dkolbly/logging"
	"github.com/dkolbly/logging/pretty"
)

var log = logging.New("hipchat-cli")

var ErrPostFailed = errors.New("posting message failed")
var ErrInsecureNotImplemented = errors.New("--insecure is not yet implemented")

type Message struct {
	From          string `json:"from,omitempty"`
	Message       string `json:"message"`
	Color         string `json:"color"`
	MessageFormat string `json:"message_format"`
	Notify        bool   `json:"notify"`
}

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
		&cli.BoolFlag{
			Name:  "html",
			Usage: "input is already in HTML format; don't transform",
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

	// we always send HTML to HipChat; the difference is in how we
	// interpret our input.  If the --html flag is specified, we
	// pass it through unchanged.  Otherwise, we do some basic
	// processing
	format := "html"
	if !c.Bool("html") {
		text = string(processPlainText([]byte(text)))
	}

	msg := &Message{
		Color:         c.String("color"),
		Message:       text,
		MessageFormat: format,
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
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))

	if c.Bool("insecure") {
		// TODO implement --insecure; it requires setting up a
		// http.Transport with an appropriate TLSClientConfig
		return ErrInsecureNotImplemented
	}

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

// urlRe is a modified form(*) of the @gruber v2 URL regex from
// https://mathiasbynens.be/demo/url-regex, which seems to be a good
// compromise between complexity and completeness (leaning towards
// fail "safe" where safe is defined as recognizing URLs).
//
// Also, note that this will successfully match only single balanced
// parentheticals which in this context is a desirable property
// because of the likelihood that someone writing something like "Hey
// @bob did you see the thing? (you know, http://bit.ly/thing)" does
// not intend the ')' to be part of the URL.  However, it is
// limited(**) in capability
//
// (** this StackOverflow answer, my all-time favorite, explains one
// reason that it is limited, although in the context of HTML parsing
// and not URL parsing: http://bit.ly/1hY5QfK)
//                                           ^ see, I just did it
//

var urlRe = regexp.MustCompile(`(?i)\b((?:https?:(?:/{1,3}|[a-z0-9%]))(?:[^\s()<>]+|\(([^\s()<>]+|(\([^\s()<>]+\)))*\))+(?:\(([^\s()<>]+|(\([^\s()<>]+\)))*\)|[^\s` + "`" + `!()\[\]{};:'".,<>?«»“”‘’]))`)

// processPlainText takes a message in plain text as input and applies
// certain transformations to HTML-ify it
func processPlainText(src []byte) []byte {

	out := &bytes.Buffer{}

	plain := func(chunk []byte) {
		out.WriteString(html.EscapeString(string(chunk)))
	}

	for {
		loc := urlRe.FindIndex(src)
		if loc == nil {
			plain(src)
			break
		}
		plain(src[:loc[0]])
		url := src[loc[0]:loc[1]]
		fmt.Fprintf(out, `<a href="%s">%s</a>`, url, url)
		src = src[loc[1]:]
	}
	return out.Bytes()
}
