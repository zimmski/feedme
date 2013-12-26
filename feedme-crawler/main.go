package main

import (
	"bytes"
	"encoding/json"
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
	ReturnOk = iota
	ReturnHelp
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

			os.Exit(ReturnHelp)
		}
	}

	if env := os.Getenv("FEEDMESPEC"); env != "" {
		opts.Spec = env
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
							logErrorWorker(id, err.Error())
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

	os.Exit(ReturnOk)
}

func processFeed(workerID int, feed *feedme.Feed) error {
	var err error

	logVerboseWorker(workerID, "fetch feed %s from %s", feed.Name, feed.URL)

	var raw map[string]*json.RawMessage
	err = json.Unmarshal([]byte(feed.Transform), &raw)
	if err != nil {
		return fmt.Errorf("cannot parse transform JSON: %s", err.Error())
	}

	var transform map[string]string
	err = json.Unmarshal(*raw["transform"], &transform)
	if err != nil {
		return fmt.Errorf("cannot parse transform element: %s", err.Error())
	}

	transformTemplates := make(map[string]*template.Template)
	for name, tem := range transform {
		transformTemplates[name], err = template.New(name).Parse(tem)
		if err != nil {
			return fmt.Errorf("cannot create transform template: %s", err.Error())
		}
	}

	jsonItems, err := jsonArray(raw["items"])
	if err != nil {
		return fmt.Errorf("cannot parse items element: %s", err.Error())
	}

	doc, err := goquery.NewDocument(feed.URL)
	if err != nil {
		return fmt.Errorf("cannot open URL: %s", err.Error())
	}

	var items []feedme.Item

	for _, rawTransform := range jsonItems {
		itemValues, err := crawlSelect(doc.Selection, rawTransform, nil)
		if err != nil {
			return fmt.Errorf("cannot transform website: %s", err.Error())
		}

		if len(itemValues[len(itemValues)-1]) == 0 {
			logVerboseWorker(workerID, "Nothing to transform")

			continue
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
					feedItem.URI = s
				default:
					return fmt.Errorf("unkown field %s", name)
				}
			}

			if feedItem.Title != "" && feedItem.URI != "" {
				logVerboseWorker(workerID, "found item %+v", feedItem)

				items = append(items, feedItem)
			}
		}
	}

	err = db.CreateItems(feed, items)
	if err != nil {
		return fmt.Errorf("cannot insert items into database: %s", err.Error())
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

			if baseSelection && i != nodes.Length()-1 && len(itemValues[len(itemValues)-1]) != 0 {
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
			return nil, fmt.Errorf("no element %s found", selector)
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
			return nil, fmt.Errorf("no attribute %s found", selector)
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
		return nil, fmt.Errorf("do not know how to transform %+v", rawTransform)
	}

	return itemValues, nil
}

func crawlStore(value string, rawTransform map[string]*json.RawMessage, itemValue map[string]interface{}) error {
	var err error

	if rawRegex, ok := rawTransform["regex"]; ok {
		if _, ok := rawTransform["matches"]; !ok {
			return fmt.Errorf("regex node requires a matches attribute")
		}

		var transformMatches []map[string]string
		err = json.Unmarshal(*rawTransform["matches"], &transformMatches)
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
			return fmt.Errorf("no matches found")
		}

		if len(matches)-1 != len(transformMatches) {
			return fmt.Errorf("unequal match count")
		}

		for i := 0; i < len(transformMatches); i++ {
			if _, ok := transformMatches[i]["name"]; !ok {
				return fmt.Errorf("match needs a name attribute")
			}
			if _, ok := transformMatches[i]["type"]; !ok {
				return fmt.Errorf("match needs a type attribute")
			}

			var name = transformMatches[i]["name"]
			var typ = transformMatches[i]["type"]

			switch typ {
			case "int":
				v, _ := strconv.Atoi(matches[i+1])

				itemValue[name] = v
			case "string":
				itemValue[name] = matches[i+1]
			default:
				return fmt.Errorf("unknown type %s", typ)
			}
		}
	} else if _, ok := rawTransform["copy"]; ok {
		if _, ok := rawTransform["name"]; !ok {
			return fmt.Errorf("copy needs a name attribute")
		}
		if _, ok := rawTransform["type"]; !ok {
			return fmt.Errorf("copy needs a type attribute")
		}

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
			return fmt.Errorf("unknown type %s", typ)
		}
	} else {
		return fmt.Errorf("do not know how to transform %+v", rawTransform)
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

	if _, ok := rawTransform["do"]; !ok {
		return "", nil, fmt.Errorf("select node needs a do attribute")
	}

	do, err := jsonArray(rawTransform["do"])
	if err != nil {
		return "", nil, err
	}

	return selector, do, nil
}

func logError(format string, a ...interface{}) (n int, err error) {
	return fmt.Printf("ERROR "+format+"\n", a...)
}

func logErrorWorker(workerID int, format string, a ...interface{}) (n int, err error) {
	return logError(fmt.Sprintf("[%d] ", workerID)+format, a...)
}

func logVerbose(format string, a ...interface{}) (n int, err error) {
	if !opts.Verbose {
		return 0, nil
	}

	return fmt.Printf("VERBOSE "+format+"\n", a...)
}

func logVerboseWorker(workerID int, format string, a ...interface{}) (n int, err error) {
	return logVerbose(fmt.Sprintf("[%d] ", workerID)+format, a...)
}
