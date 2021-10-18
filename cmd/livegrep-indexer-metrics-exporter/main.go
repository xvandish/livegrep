package main

import (
	"flag"
	"io"
	"log"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	statsd "gopkg.in/alexcesaro/statsd.v2"
)

const (
	envStatsdAddr        = "LIVEGREP_METRICS_STATSD_ADDRESS"
	envStatsdPrefix      = "LIVEGREP_METRICS_STATSD_PREFIX"
	envStatsdFlushPeriod = "LIVEGREP_METRICS_STATSD_FLUSH_PERIOD"
)

var (
	flagStatsdAddr   = flag.String("statsd-address", os.Getenv(envStatsdAddr), "address URI of statsd listener for metrics export")
	flagStatsdPrefix = flag.String("statsd-prefix", os.Getenv(envStatsdPrefix), "optional prefix to apply to all metrics")
	flagFlushPeriod  = flag.Duration("statsd-flush-period", time.Millisecond*100, "how often to flush stats")
	flagTagFormat    = flag.String("statsd-tag-format", "", "format for tags, if using. Must be either \"InfluxDB\" or \"Datadog\"")

	// flagStatsdTags = newStringMapFlag()

	indexTimeToCompletePattern = regexp.MustCompile("repository[\\s]*indexed[\\s]*in[\\s]*(.*)")
	metricPattern              = regexp.MustCompile(strings.Join([]string{
		"([\\w\\.]+)", // Metric name (alphabetic characters and dots)
		"\\s",         // Separator
		"(\\d+)",      // Metric value (gauge)
	}, ""))
	metricsDumpPattern = regexp.MustCompile("(?s)== begin metrics ==\\s*(.+)\\s== end metrics ==")
)

func init() {
	// flag.Var(flagStatsdTags, "statsd-tag", "statsd tags to include on all emitted metrics. Accepted formats are InfluxDB and Datadog")

	// if *flagStatsdTags != nil && *flagTagFormat == "" {
	// 	log.Fatalf("If using tags, tag format must be specified")
	// }

	flag.Parse()

	if *flagStatsdAddr == "" {
		log.Fatalf("no statsd target address specified")
	} else {
		log.Printf("using statsd server: address=%s", *flagStatsdAddr)
	}

	if *flagStatsdPrefix != "" {
		log.Printf("using prefix for all metrics: prefix=%s", *flagStatsdPrefix)
	}
}

func main() {
	log.Println("starting livegrep statsd metrics exporter")

	// TODO: add tags support in client creation
	startTime := time.Now()

	client, err := statsd.New(
		// statsd.Address(*flagStatsdAddr), // connect by default
		statsd.Prefix(*flagStatsdPrefix),
		// statsd.Tags(*&flagStatsdTags),
		// statsd.TagsFormat(*flagTagFormat),
	)

	log.Printf("client is: %v\n", client)
	log.Printf("err is: %v\n", err)

	client.Increment("some-bucket")
	client.Gauge("num_goroutine", runtime.NumGoroutine())

	if err != nil {
		panic(err)
	}

	defer client.Close()

	// Read the index builder logs from standard input
	// can't just read the last x lines because multiple things may be writing to std-input

	log.Printf("before io.RadAll")
	indexLogs, err := io.ReadAll(os.Stdin)
	log.Printf("after io.ReadAll")
	log.Printf("indexLogs: %v\n", indexLogs)
	if err != nil {
		log.Fatalf("failed to read index builder logs from stdin: %s", err.Error())
	}

	// Regex-match the Repository indexed in ... line
	iTimeToCompleteLine := indexTimeToCompletePattern.FindStringSubmatch(string(indexLogs))

	if iTimeToCompleteLine == nil {
		log.Fatal("failed to read time to index line from stdin")
	}

	timeToIndex, err := time.ParseDuration(string(iTimeToCompleteLine[1]))

	if err != nil {
		log.Fatalf("failed to parse indexing time %v", err)
	}

	client.Timing("index.timeToCreate", timeToIndex.Milliseconds())

	// Regex-match the metrics dump block
	dump := metricsDumpPattern.FindStringSubmatch(string(indexLogs))
	if len(dump) < 2 {
		log.Fatalf("failed to parse metrics dump from indexer output")
	}

	metrics := make(map[string]int)
	for _, metricLine := range strings.Split(dump[1], "\n") {
		metric := metricPattern.FindStringSubmatch(metricLine)
		if len(metric) < 3 {
			log.Fatalf("failed to parse metric name and value: line=%s", metricLine)
		}
		// TODO: revist the array access bit here

		value, err := strconv.Atoi(metric[2])
		if err != nil {
			log.Fatalf("failed to parse metric value: name=%s value=%s", metric[1], metric[2])
		}

		metrics[metric[1]] = value
	}

	log.Printf("metrics are: %v\n", metrics)

	// Report all of the metrics we just collected
	for metric, value := range metrics {
		log.Printf("reporting gauge metric: metric=%s value=%d", metric, value)
		client.Gauge(metric, float64(value))
	}

	// Report how long it took to export the metrics + initialize the client
	completionTime := time.Since(startTime)
	log.Printf("initialized client & completed metrics export: duration=%s", completionTime)
	// library expexts timing value in ms
	// https://github.com/alexcesaro/statsd/blob/v2.0.0/statsd.go#L108
	client.Timing("export.duration", completionTime.Milliseconds())
}
