package main

import (
	geoipc "github.com/rubiojr/freegeoip-client"
	mqttc "./utils/mqtt"
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"gopkg.in/alecthomas/kingpin.v1"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type Host struct {
	IP          string  `json:"ip"`
	Name        string  `json:"hostname"`
	Hop         int     `json:"hop-number"`
	Sent        int     `json:"sent"`
	LostPercent float64 `json:"lost-percent"`
	Last        float64 `json:"mean"`
	Avg         float64 `json:"mean"`
	Best        float64 `json:"best"`
	Worst       float64 `json:"worst"`
	StDev       float64 `json:"standard-dev"`
}

type Report struct {
	Time        time.Time     `json:"time"`
	Hosts       []*Host       `json:"hosts"`
	Hops        int           `json:"hops"`
	ElapsedTime time.Duration `json:"elapsed_time"`
	Location    geoipc.Location `json:"location"`
}

func NewReport(reportCycles int, host string, args ...string) *Report {
	report := &Report{}
	report.Time = time.Now()
	args = append([]string{"--report", "-c", strconv.Itoa(reportCycles), host}, args...)

	tstart := time.Now()
	rawOutput, err := exec.Command("mtr", args...).Output()

	if err != nil {
		panic("Error running the mtr command")
	}

	buf := bytes.NewBuffer(rawOutput)
	scanner := bufio.NewScanner(buf)
	scanner.Split(bufio.ScanLines)

	skipHeader := 2
	for scanner.Scan() {
		if skipHeader != 0 {
			skipHeader -= 1
			continue
		}

		tokens := strings.Fields(scanner.Text())
		sent, err := strconv.Atoi(tokens[3])
		if err != nil {
			panic("Error parsing sent field")
		}

		host := Host{
			IP:   tokens[1],
			Sent: sent,
		}

		f2F(strings.Replace(tokens[2], "%", "", -1), &host.LostPercent)
		f2F(tokens[4], &host.Last)
		f2F(tokens[5], &host.Avg)
		f2F(tokens[6], &host.Best)
		f2F(tokens[7], &host.Worst)
		f2F(tokens[8], &host.StDev)

		report.Hosts = append(report.Hosts, &host)
	}

	report.Hops = len(report.Hosts)
	report.ElapsedTime = time.Since(tstart)
	loc, err := geoipc.GetLocation()
	if err != nil {
		report.Location = geoipc.Location{}
	} else {
		report.Location = loc
	}

	return report
}

func f2F(val string, field *float64) {
	f, err := strconv.ParseFloat(val, 64)
	*field = f
	if err != nil {
		panic("Error parsing field")
	}
}

func run(count int, host, brokerUrl, topic string) error {
	r := NewReport(count, host, "-n")
	msg, _ := json.Marshal(r)
	err := mqttc.PushMsg("push-mtr", brokerUrl, topic, string(msg))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error sending report: %s", err)
	}
	return err
}

func main() {
	count := kingpin.Flag("count", "Report cycles (mtr -c)").
		Default("10").Int()

	topic := kingpin.Flag("topic", "MTTQ topic").Default("/metrics/mtr").
		String()

	host := kingpin.Arg("host", "Target host").Required().String()

	repeat := kingpin.Flag("repeat", "Send the report every X seconds").
		Default("0").Int()

	brokerUrl := kingpin.Flag("broker-url", "MQTT broker URL").
		Default("").String()

	kingpin.Version("0.1")
	kingpin.Parse()

	if *brokerUrl == "" {
		*brokerUrl = os.Getenv("MQTT_URL")
		if *brokerUrl == "" {
			fmt.Fprintf(os.Stderr, "Invalid MQTT broker URL")
		}
	}

	if *repeat != 0 {
		timer := time.NewTicker(1 * time.Second)
		for _ = range timer.C {
			run(*count, *host, *brokerUrl, *topic)
		}
	} else {
		err := run(*count, *host, *brokerUrl, *topic)
		if err != nil {
			os.Exit(1)
		}
	}
}
