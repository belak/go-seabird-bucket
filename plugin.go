package bucket

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/belak/go-seabird"
	"github.com/belak/go-seabird/plugins"
	"github.com/belak/nut"
	"github.com/go-irc/irc"
)

func init() {
	seabird.RegisterPlugin("bucket", newBucketPlugin)
}

var (
	// These are roughly in the order that they appear in xkcd-Bucket
	literalRegexp = regexp.MustCompile(`(?i)^literal(?:\[(\d+)\])? (.*)$`)
	undoRegexp    = regexp.MustCompile(`(?i)^undo(?: last)?$`)
	mergeRegexp   = regexp.MustCompile(`(?i)^merge (.*) [-=]> (.*)$`)
	aliasRegexp   = regexp.MustCompile(`(?i)^alias (.*) [-=]> (.*)$`)
	lookupRegexp  = regexp.MustCompile(`(?i)^lookup (.*)$`)
	forgetRegexp  = regexp.MustCompile(`(?i)^forget (.*)$`)
	whatRegexp    = regexp.MustCompile(`(?i)^what was that\??$`)

	// Variable commands
	//
	// TODO: Clean these up so we don't need to use a regexp for all of them.
	listVarsRegexp  = regexp.MustCompile(`(?i)^list vars$`)
	listVarRegexp   = regexp.MustCompile(`(?i)^list var (\w+)$`)
	removeValRegexp = regexp.MustCompile(`(?i)^remove value (\w+) (.*)$`)
	addValRegexp    = regexp.MustCompile(`(?i)^add value (\w+) (.*)$`)
	createVarRegexp = regexp.MustCompile(`(?i)^create var (\w+)$`)
	removeVarRegexp = regexp.MustCompile(`(?i)^remove var (\w+)$`)

	// Inventory commands
	fullInventoryRegexp = regexp.MustCompile(`(?i)^(?:detailed inventory|list item details)$`)
	inventoryRegexp     = regexp.MustCompile(`(?i)^(?:inventory|list items)$`)

	isRegexp = regexp.MustCompile(`(?i)^(.+?) (is|is also|are|<\w+>) (.+)$`)
)

type bucketPlugin struct {
	db      *nut.DB
	tracker *plugins.ChannelTracker

	AdminModes string

	// The last action which modified the database and the last phrase which
	// we mentioned.
	lastActionID string
	lastFoundID  string
}

type bucketMessage struct {
	Data   string // Contents of the message
	Who    string // Who the message was from
	OP     bool   // If the user has OP permissions on the bot
	Public bool
	Target string
}

// This is the core bucket type which we store in the database
type bucketFact struct {
	Responses []bucketFactResponse
}

type bucketFactResponse struct {
	Text    string
	Creator string
	Verb    string
}

type bucketVariable struct {
	Values []bucketValue
}

type bucketValue struct {
	Text    string
	Creator string
}

func newBucketPlugin(b *seabird.Bot, bm *seabird.BasicMux, mm *seabird.MentionMux, tracker *plugins.ChannelTracker, db *nut.DB) error {
	bp := &bucketPlugin{
		db:      db,
		tracker: tracker,
	}

	err := db.Update(func(tx *nut.Tx) error {
		b, err := tx.CreateBucketIfNotExists("bucket")
		if err != nil {
			return err
		}

		_, err = b.CreateBucketIfNotExists("facts")
		if err != nil {
			return err
		}

		_, err = b.CreateBucketIfNotExists("vars")
		return err
	})
	if err != nil {
		return err
	}

	err = b.Config("bucket", bp)
	if err != nil {
		return err
	}

	// The mention mux is the most important one here, as we use this for most
	// commands. The basic mux is used when seabird is not mentioned. If
	// seabird is mentioned and the user is not using some sort of command, it
	// will fall back to the general channel handling and finally give a
	// specified response if there were no factoids.
	mm.Event(bp.mentionCallback)

	return nil
}

func (p *bucketPlugin) mentionCallback(b *seabird.Bot, m *irc.Message) {
	// Pull all the info we can out of the message
	bm := &bucketMessage{
		Data:   strings.TrimSpace(m.Trailing()),
		Who:    m.Prefix.User,
		Public: b.FromChannel(m),
		Target: m.Params[0],
	}

	if bm.Public {
		user := p.tracker.LookupUser(bm.Who)
		userModes := user.ModesInChannel(bm.Target)
		for _, mode := range p.AdminModes {
			if userModes[mode] {
				bm.OP = true
				break
			}
		}
	}

	fmt.Printf("%+v\n", bm)

	if match := literalRegexp.FindStringSubmatch(bm.Data); len(match) > 0 {
		// match[1] - number or *
		// match[2] - thing to look up
		//
		// TODO: Handle match[1]

		key := strings.ToLower(match[2])

		out := &bucketFact{}
		_ = p.db.View(func(tx *nut.Tx) error {
			bucket := tx.Bucket("bucket").Bucket("facts")
			return bucket.Get(key, out)
		})

		b.MentionReply(m, "%+v", out)
	} else if match := undoRegexp.FindStringSubmatch(bm.Data); len(match) > 0 {
	} else if match := mergeRegexp.FindStringSubmatch(bm.Data); len(match) > 0 {
		// match[1] - merge
		// match[2] - target
	} else if match := aliasRegexp.FindStringSubmatch(bm.Data); len(match) > 0 {
		// match[1] - alias
		// match[2] - target
	} else if match := lookupRegexp.FindStringSubmatch(bm.Data); len(match) > 0 {
		// match[1] - lookup string
	} else if match := forgetRegexp.FindStringSubmatch(bm.Data); len(match) > 0 {
		// match[1] - lookup string
	} else if match := whatRegexp.FindStringSubmatch(bm.Data); len(match) > 0 {
	} else if match := listVarsRegexp.FindStringSubmatch(bm.Data); len(match) > 0 {
	} else if match := listVarRegexp.FindStringSubmatch(bm.Data); len(match) > 0 {
		// match[1] - variable
	} else if match := removeValRegexp.FindStringSubmatch(bm.Data); len(match) > 0 {
		// match[1] - variable
		// match[2] - value
	} else if match := addValRegexp.FindStringSubmatch(bm.Data); len(match) > 0 {
		// match[1] - variable
		// match[2] - value
	} else if match := createVarRegexp.FindStringSubmatch(bm.Data); len(match) > 0 {
		// match[1] - variable
	} else if match := removeVarRegexp.FindStringSubmatch(bm.Data); len(match) > 0 {
		// match[1] - variable
	} else if match := fullInventoryRegexp.FindStringSubmatch(bm.Data); len(match) > 0 {
	} else if match := inventoryRegexp.FindStringSubmatch(bm.Data); len(match) > 0 {
	} else if match := isRegexp.FindStringSubmatch(bm.Data); len(match) > 0 {
		// match[1] - word
		// match[2] - is|is also|are|<\w+>
		// match[3] - description

		key := strings.ToLower(match[1])

		verb := match[2]
		if verb == "is also" {
			verb = "is"
		} else if verb[0] == '<' {
			verb = verb[1 : len(verb)-1]
		}

		out := &bucketFact{}
		resp := bucketFactResponse{
			Text:    match[3],
			Creator: bm.Who,
			Verb:    verb,
		}
		_ = p.db.Update(func(tx *nut.Tx) error {
			bucket := tx.Bucket("bucket").Bucket("facts")
			bucket.Get(key, out)
			out.Responses = append(out.Responses, resp)
			return bucket.Put(key, out)
		})

		// TODO: Look this up from a fact, falling back to this if need be.
		b.Reply(m, "Ok %s, %s %s %s", bm.Who, match[1], resp.Verb, resp.Text)
	} else {
		// Attempt lookup
	}
}
