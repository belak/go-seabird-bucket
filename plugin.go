package bucket

import (
	"bytes"
	"fmt"
	"math/rand"
	"os"
	"regexp"
	"strings"

	"github.com/Sirupsen/logrus"
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
	literalRegexp  = regexp.MustCompile(`(?i)^literal(?:\[(\d+)\])? (.*)$`)
	undoRegexp     = regexp.MustCompile(`(?i)^undo(?: last)?$`)
	mergeRegexp    = regexp.MustCompile(`(?i)^merge (.*) [-=]> (.*)$`)
	aliasRegexp    = regexp.MustCompile(`(?i)^alias (.*) [-=]> (.*)$`)
	lookupRegexp   = regexp.MustCompile(`(?i)^lookup (.*)$`)
	forgetIsRegexp = regexp.MustCompile(`(?i)^forget (.+?) (is|is also|are|<\w+>) (.+)$`) // Custom feature
	forgetRegexp   = regexp.MustCompile(`(?i)^forget (.*)$`)
	whatRegexp     = regexp.MustCompile(`(?i)^what was that\??$`)

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

	renderRegexp = regexp.MustCompile(`(?i)^render (.*)$`) // Custom feature
	isRegexp     = regexp.MustCompile(`(?i)^(.+?) (is|is also|are|<\w+>) (.+)$`)
)

type bucketPlugin struct {
	db      *nut.DB
	tracker *plugins.ChannelTracker

	AdminModes string
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
	Values  []bucketValue
	Creator string
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
	logger := b.GetLogger()

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
	} else if match := forgetIsRegexp.FindStringSubmatch(bm.Data); len(match) > 0 {
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

		var found bool
		out := &bucketFact{}
		_ = p.db.Update(func(tx *nut.Tx) error {
			bucket := tx.Bucket("bucket").Bucket("facts")
			bucket.Get(key, out)
			for k, v := range out.Responses {
				if v.Text == match[3] && v.Verb == verb {
					found = true
					out.Responses = append(out.Responses[:k], out.Responses[k+1:]...)
					break
				}
			}
			return bucket.Put(key, out)
		})

		if found {
			logger.WithFields(logrus.Fields{
				"key":  match[1],
				"verb": verb,
				"text": match[3],
			}).Info("Removed fact")

			// TODO: Look this up from a fact, falling back to this response if need be.
			b.Reply(m, "Ok %s, forgot %s %s %s", bm.Who, match[1], verb, match[3])
		} else {
			// TODO: Look this up from a fact, falling back to this response if need be.
			b.Reply(m, "Ok %s, couldn't find %s %s %s", bm.Who, match[1], verb, match[3])
		}
	} else if match := forgetRegexp.FindStringSubmatch(bm.Data); len(match) > 0 {
		// match[1] - lookup string
	} else if match := whatRegexp.FindStringSubmatch(bm.Data); len(match) > 0 {
	} else if match := listVarsRegexp.FindStringSubmatch(bm.Data); len(match) > 0 {
	} else if match := listVarRegexp.FindStringSubmatch(bm.Data); len(match) > 0 {
		// match[1] - variable

		key := strings.ToLower(match[1])
		out := &bucketVariable{}
		_ = p.db.View(func(tx *nut.Tx) error {
			bucket := tx.Bucket("bucket").Bucket("vars")
			return bucket.Get(key, out)
		})

		data := &bytes.Buffer{}
		var first bool
		for _, v := range out.Values {
			if !first {
				data.WriteString(", ")
			}
			data.WriteString(v.Text)
		}

		b.Reply(m, "Ok %s, %s is %s", bm.Who, key, data.String())
	} else if match := removeValRegexp.FindStringSubmatch(bm.Data); len(match) > 0 {
		// match[1] - variable
		// match[2] - value
	} else if match := addValRegexp.FindStringSubmatch(bm.Data); len(match) > 0 {
		// match[1] - variable
		// match[2] - value

		key := strings.ToLower(match[1])
		out := &bucketVariable{}
		val := bucketValue{
			Text:    match[2],
			Creator: bm.Who,
		}
		err := p.db.Update(func(tx *nut.Tx) error {
			bucket := tx.Bucket("bucket").Bucket("vars")
			err := bucket.Get(key, out)
			if err != nil {
				return err
			}

			out.Values = append(out.Values, val)
			return bucket.Put(key, out)
		})
		if err != nil {
			b.Reply(m, "Ok %s, %s", err.Error())
			return
		}

		logger.WithFields(logrus.Fields{
			"name": key,
			"text": val.Text,
		}).Info("Added value to variable")

		// TODO: Look this up from a fact, falling back to this response if need be.
		b.Reply(m, "Ok %s, added %s to variable %s", bm.Who, val.Text, key)
	} else if match := createVarRegexp.FindStringSubmatch(bm.Data); bm.OP && len(match) > 0 {
		// match[1] - variable
		key := strings.ToLower(match[1])
		out := &bucketVariable{}
		var created bool
		_ = p.db.Update(func(tx *nut.Tx) error {
			bucket := tx.Bucket("bucket").Bucket("vars")
			bucket.Get(key, out)
			if out.Creator == "" {
				created = true
				out.Creator = bm.Who
			}
			return bucket.Put(key, out)
		})

		if created {
			logger.WithFields(logrus.Fields{
				"name": key,
			}).Info("Created variable")

			// TODO: Look this up from a fact, falling back to this response if need be.
			b.Reply(m, "Ok %s, created variable %s", bm.Who, key)
		} else {
			// TODO: Look this up from a fact, falling back to this response if need be.
			b.Reply(m, "Ok %s, variable %s already created", bm.Who, key)
		}
	} else if match := removeVarRegexp.FindStringSubmatch(bm.Data); bm.OP && len(match) > 0 {
		// match[1] - variable
		key := strings.ToLower(match[1])
		out := &bucketVariable{}
		_ = p.db.Update(func(tx *nut.Tx) error {
			bucket := tx.Bucket("bucket").Bucket("vars")
			bucket.Get(key, out)
			return bucket.Delete(key)
		})

		if out.Creator != "" {
			logger.WithFields(logrus.Fields{
				"name": key,
			}).Info("Removed variable")

			// TODO: Look this up from a fact, falling back to this response if need be.
			b.Reply(m, "Ok %s, removed variable %s", bm.Who, key)
		} else {
			// TODO: Look this up from a fact, falling back to this response if need be.
			b.Reply(m, "Ok %s, variable %s doesn't exist", bm.Who, key)
		}
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

		logger.WithFields(logrus.Fields{
			"key":  match[1],
			"verb": resp.Verb,
			"text": resp.Text,
		}).Info("Stored fact")

		// TODO: Look this up from a fact, falling back to this response if need be.
		b.Reply(m, "Ok %s, %s %s %s", bm.Who, match[1], resp.Verb, resp.Text)
	} else if match := renderRegexp.FindStringSubmatch(bm.Data); len(match) > 0 {
		text := os.Expand(match[1], func(key string) string {
			outVar := &bucketVariable{}
			_ = p.db.View(func(tx *nut.Tx) error {
				bucket := tx.Bucket("bucket").Bucket("vars")
				return bucket.Get(key, outVar)
			})
			if len(outVar.Values) == 0 {
				return ""
			}
			return outVar.Values[rand.Intn(len(outVar.Values))].Text
		})
		b.MentionReply(m, "%s", text)
	} else {
		// Attempt lookup
	}
}
