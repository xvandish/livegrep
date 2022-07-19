package main

import (
	"flag"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/alexcesaro/statsd.v2"
)

// The shape of this program is all credit to Kevin Lin's fork of livegrep. Source at:
// https://source.static.kevinlin.info/external/livegrep/file/cmd/livegrep-metrics-exporter/main.go

const (
	envStatsdAddr   = "LIVEGREP_METRICS_STATSD_ADDRESS"
	envStatsdPrefix = "LIVEGREP_METRICS_STATSD_PREFIX"
)

var (
	flagStatsdAddr   = flag.String("statsd-address", os.Getenv(envStatsdAddr), "address URI of statsd listener for metrics export")
	flagStatsdPrefix = flag.String("statsd-prefix", os.Getenv(envStatsdPrefix), "optional prefix to apply to all metrics")
	flagMetricsPath  = flag.String("metrics-out", "", "path to the file containing indexing metrics")
	flagStatsdTags   = newStringMapFlag()

	indexTimeToCompletePattern = regexp.MustCompile("repository[\\s]*indexed[\\s]*in[\\s]*(.*)")
	metricPattern              = regexp.MustCompile(strings.Join([]string{
		"([\\w\\.]+)", // Metric name (alphabetic characters and dots)
		"\\s",         // Separator
		"(\\d+)",      // Metric value (gauge)
	}, ""))
	metricsDumpPattern = regexp.MustCompile("(?s)== begin metrics ==\\s*(.+)\\s== end metrics ==")
)

func init() {
	flag.Var(flagStatsdTags, "statsd-tag", "statsd tags to include on all emitted metrics")

	flag.Parse()

	if *flagMetricsPath == "" {
		log.Fatalf("--metrics-out is required. It is the path to the file containing metrics from the indexing run\n")
	}

	if *flagStatsdAddr == "" {
		log.Fatalf("--statsd-address is required. It is the host:port of the StatsD server.\n")
	} else {
		log.Printf("using statsd server: address=%s", *flagStatsdAddr)
	}

	if *flagStatsdPrefix != "" {
		log.Printf("using prefix for all metrics: prefix=%s", *flagStatsdPrefix)
	}
}

func main() {
	log.Println("starting livegrep statsd metrics exporter")

	// Stopwatch to track the end-to-end duration required to export metrics
	start := time.Now()

	// Create a statsd client
	statsd, err := statsd.New(statsd.Address(*flagStatsdAddr), statsd.Prefix(*flagStatsdPrefix))
	if err != nil {
		panic(err)
	}
	defer statsd.Close()

	metricsFile, err := os.ReadFile(*flagMetricsPath)
	if err != nil {
		panic(err)
	}

	metricsFileStr := string(metricsFile)

	iTimeToCompleteLine := indexTimeToCompletePattern.FindStringSubmatch(metricsFileStr)
	if iTimeToCompleteLine == nil {
		log.Fatal("failed to read time to index line from stdin")
	}

	timeToIndex, err := time.ParseDuration(string(iTimeToCompleteLine[1]))

	if err != nil {
		log.Fatalf("failed to parse indexing time %v", err)
	}
	statsd.Timing("index.timeToIndex", timeToIndex.Milliseconds())

	// Regex-match the metrics dump block
	dump := metricsDumpPattern.FindStringSubmatch(metricsFileStr)
	if len(dump) < 2 {
		log.Fatalf("failed to parse metrics dump from indexer output")
	}

	// Regex-match the metric name and value from each line
	metrics := make(map[string]int)
	for _, metricLine := range strings.Split(dump[1], "\n") {
		metric := metricPattern.FindStringSubmatch(metricLine)
		if len(metric) < 3 {
			log.Fatalf("failed to parse metric name and value: line=%s", metricLine)
		}

		value, err := strconv.Atoi(metric[2])
		if err != nil {
			log.Fatalf("failed to parse metric value: name=%s value=%s", metric[1], metric[2])
		}

		metrics[metric[1]] = value
	}

	// Report all parsed gauge metrics to statsd
	for metric, value := range metrics {
		log.Printf("reporting gauge metric: metric=%s value=%d", metric, value)
		statsd.Gauge(metric, float64(value))
	}

	// Report metrics export duration to statsd
	duration := time.Since(start)
	log.Printf("completed metrics export: duration=%v", duration)
	statsd.Timing("export.duration", duration.Milliseconds())
}
