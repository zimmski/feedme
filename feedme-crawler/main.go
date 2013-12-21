package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"text/template"

	"github.com/PuerkitoBio/goquery"
	"github.com/jessevdk/go-flags"

	"github.com/zimmski/feedme"
	"github.com/zimmski/feedme/backend"
)

const (
	RET_OK = iota
	RET_HELP
)

var db backend.Backend
var opts struct {
	MaxIdleConns int    `long:"max-idle-conns" default:"10" description:"Max idle connections of the database"`
	MaxOpenConns int    `long:"max-open-conns" default:"10" description:"Max open connections of the database"`
	Spec         string `short:"s" long:"spec" default:"dbname=feedme sslmode=disable" description:"The database connection spec"`
	Workers      int    `short:"w" long:"workers" default:"1" description:"Worker count for processing feeds"`
	Verbose      bool   `short:"v" long:"verbose" description:"Print what is going on"`
}

func main() {
	var err error

	p := flags.NewNamedParser("feedme-crawler", flags.HelpFlag)
	p.ShortDescription = "The feedme crawler"
	p.AddGroup("Crawler arguments", "", &opts)

	_, err = p.ParseArgs(os.Args)
	if err != nil {
		if e, ok := err.(*flags.Error); !ok || e.Type != flags.ErrHelp {
			panic(err)
		} else {
			p.WriteHelp(os.Stdout)

			os.Exit(RET_HELP)
		}
	}

	runtime.GOMAXPROCS(runtime.NumCPU())

	db, err = backend.NewBackend("postgresql")
	if err != nil {
		panic(err)
	}

	err = db.Init(backend.BackendParameters{
		Spec:         opts.Spec,
		MaxIdleConns: opts.MaxIdleConns,
		MaxOpenConns: opts.MaxOpenConns,
	})
	if err != nil {
		panic(err)
	}

	feeds, err := db.SearchFeeds()
	if err != nil {
		panic(err)
	}

	feedQueue := make(chan feedme.Feed)
	consumeFeeds := make(chan bool, len(feeds))

	for i := 0; i < opts.Workers; i++ {
		go func(id int, feedQueue <-chan feedme.Feed, consumeFeeds chan<- bool) {
			for {
				select {
				case feed, ok := <-feedQueue:
					if ok {
						err := processFeed(id, &feed)
						if err != nil {
							EW(id, err.Error())
						}

						consumeFeeds <- true
					} else {
						return
					}
				}
			}
		}(i, feedQueue, consumeFeeds)
	}

	for _, feed := range feeds {
		feedQueue <- feed
	}

	for i := 0; i < len(feeds); i++ {
		<-consumeFeeds
	}

	close(feedQueue)

	os.Exit(RET_OK)
}

func processFeed(workerId int, feed *feedme.Feed) error {
	var err error

	if opts.Verbose {
		VW(workerId, "Fetch feed %s from %s", feed.Name, feed.Url)
	}

	var raw map[string]*json.RawMessage
	err = json.Unmarshal([]byte(feed.Transform), &raw)
	if err != nil {
		return newError("Cannot parse transform JSON: %s", err.Error())
	}

	var transform map[string]string
	err = json.Unmarshal(*raw["transform"], &transform)
	if err != nil {
		return newError("Cannot parse transform element: %s", err.Error())
	}

	transformTemplates := make(map[string]*template.Template)
	for name, tem := range transform {
		transformTemplates[name], err = template.New(name).Parse(tem)
		if err != nil {
			return newError("Cannot create transform template: %s", err.Error())
		}
	}

	jsonItems, err := jsonArray(raw["items"])
	if err != nil {
		return newError("Cannot parse items element: %s", err.Error())
	}

	doc, err := goquery.NewDocument(feed.Url)
	if err != nil {
		return newError("Cannot open URL: %s", err.Error())
	}

	var items []feedme.Item

	for _, rawTransform := range jsonItems {
		itemValues, err := crawlSelect(doc.Selection, rawTransform, nil)
		if err != nil {
			return newError("Cannot transform website: %s", err.Error())
		}

		for _, itemValue := range itemValues {
			feedItem := feedme.Item{}

			for name, t := range transformTemplates {
				var out bytes.Buffer
				t.Execute(&out, itemValue)
				s := out.String()

				switch name {
				case "description":
					feedItem.Description = s
				case "title":
					feedItem.Title = s
				case "uri":
					feedItem.Uri = s
				default:
					return newError("Unkown field %s", name)
				}
			}

			if feedItem.Title != "" && feedItem.Uri != "" {
				if opts.Verbose {
					VW(workerId, "Found item %+v", feedItem)
				}

				items = append(items, feedItem)
			}
		}
	}

	err = db.CreateItems(feed, items)
	if err != nil {
		return newError("Cannot insert items into database: %s", err.Error())
	}

	return nil
}

func crawlSelect(element *goquery.Selection, rawTransform map[string]*json.RawMessage, itemValues []map[string]interface{}) ([]map[string]interface{}, error) {
	baseSelection := false

	if itemValues == nil {
		baseSelection = true

		itemValues = make([]map[string]interface{}, 1)
		// TODO finde out why this is needed as itemValues with make of length 1 has already a map shown printed with %+v. But it is nil if it is accessed
		itemValues[0] = make(map[string]interface{})
	}

	if rawSelector, ok := rawTransform["search"]; ok {
		selector, do, err := jsonSelectNode(rawTransform, rawSelector)
		if err != nil {
			return nil, err
		}

		nodes := element.Find(selector)

		nodes.Each(func(i int, s *goquery.Selection) {
			for _, d := range do {
				_, err = crawlSelect(s, d, itemValues)
				if err != nil {
					return
				}
			}

			if baseSelection && i != nodes.Length()-1 {
				itemValues = append(itemValues, make(map[string]interface{}))
			}
		})
		if err != nil {
			return nil, err
		}
	} else if rawSelector, ok := rawTransform["find"]; ok {
		selector, do, err := jsonSelectNode(rawTransform, rawSelector)
		if err != nil {
			return nil, err
		}

		s := element.Find(selector)
		if s == nil {
			return nil, newError("No item found")
		}

		for _, d := range do {
			_, err = crawlSelect(s, d, itemValues)
			if err != nil {
				return nil, err
			}
		}
	} else if rawSelector, ok := rawTransform["attr"]; ok {
		selector, do, err := jsonSelectNode(rawTransform, rawSelector)
		if err != nil {
			return nil, err
		}

		attrValue, ok := element.Attr(selector)
		if !ok {
			return nil, newError("No attr found")
		}

		for _, d := range do {
			err = crawlStore(attrValue, d, itemValues[len(itemValues)-1])
			if err != nil {
				return nil, err
			}
		}
	} else if _, ok := rawTransform["text"]; ok {
		_, do, err := jsonSelectNode(rawTransform, nil)
		if err != nil {
			return nil, err
		}

		text := element.Text()

		for _, d := range do {
			err = crawlStore(text, d, itemValues[len(itemValues)-1])
			if err != nil {
				return nil, err
			}
		}
	} else {
		return nil, newError("Do not know how to transform %+v", rawTransform)
	}

	return itemValues, nil
}

func crawlStore(value string, rawTransform map[string]*json.RawMessage, itemValue map[string]interface{}) error {
	var err error

	if rawRegex, ok := rawTransform["regex"]; ok {
		var transformMatches []map[string]string
		err = json.Unmarshal(*rawTransform["data"], &transformMatches)
		if err != nil {
			return err
		}

		reg, err := jsonString(rawRegex)
		if err != nil {
			return err
		}

		re := regexp.MustCompile(reg)
		var matches = re.FindStringSubmatch(value)

		if matches == nil {
			return newError("No matches found")
		}

		if len(matches)-1 != len(transformMatches) {
			return newError("Unequal match count")
		}

		for i := 0; i < len(transformMatches); i++ {
			var name = transformMatches[i]["name"]
			var typ = transformMatches[i]["type"]

			switch typ {
			case "int":
				v, _ := strconv.Atoi(matches[i+1])

				itemValue[name] = v
			case "string":
				itemValue[name] = matches[i+1]
			default:
				return newError("Unknown type %s", typ)
			}
		}
	} else if _, ok := rawTransform["copy"]; ok {
		name, err := jsonString(rawTransform["name"])
		if err != nil {
			return err
		}

		typ, err := jsonString(rawTransform["type"])
		if err != nil {
			return err
		}

		switch typ {
		case "int":
			v, _ := strconv.Atoi(value)

			itemValue[name] = v
		case "string":
			itemValue[name] = value
		default:
			return newError("Unknown type %s", typ)
		}
	} else {
		return newError("Do not know how to transform %+v", rawTransform)
	}

	return nil
}

func jsonArray(raw *json.RawMessage) ([]map[string]*json.RawMessage, error) {
	var array []map[string]*json.RawMessage

	err := json.Unmarshal(*raw, &array)
	if err != nil {
		return nil, err
	}

	return array, nil
}

func jsonHash(raw *json.RawMessage) (map[string]*json.RawMessage, error) {
	var hash map[string]*json.RawMessage

	err := json.Unmarshal(*raw, &hash)
	if err != nil {
		return nil, err
	}

	return hash, nil
}

func jsonString(raw *json.RawMessage) (string, error) {
	if raw == nil {
		return "", nil
	}

	var s string

	err := json.Unmarshal(*raw, &s)
	if err != nil {
		return "", err
	}

	return s, nil
}

func jsonSelectNode(rawTransform map[string]*json.RawMessage, rawSelector *json.RawMessage) (string, []map[string]*json.RawMessage, error) {
	selector, err := jsonString(rawSelector)
	if err != nil {
		return "", nil, err
	}

	do, err := jsonArray(rawTransform["do"])
	if err != nil {
		return "", nil, err
	}

	return selector, do, nil
}

func newError(format string, a ...interface{}) error {
	return errors.New(fmt.Sprintf(format, a...))
}

func E(format string, a ...interface{}) (n int, err error) {
	return fmt.Printf("ERROR "+format+"\n", a...)
}

func EW(workerId int, format string, a ...interface{}) (n int, err error) {
	return E(fmt.Sprintf("[%d] ", workerId)+format, a...)
}

func V(format string, a ...interface{}) (n int, err error) {
	return fmt.Printf("VERBOSE "+format+"\n", a...)
}

func VW(workerId int, format string, a ...interface{}) (n int, err error) {
	return V(fmt.Sprintf("[%d] ", workerId)+format, a...)
}
