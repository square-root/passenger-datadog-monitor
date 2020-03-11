package main

import (
	"bytes"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/theckman/godspeed"
	"golang.org/x/net/html/charset"
)

const (
	// DefaultHost is 127.0.0.1 (localhost)
	DefaultHost = godspeed.DefaultHost

	// DefaultPort is 8125
	DefaultPort = godspeed.DefaultPort

	// DefaultAutoTruncate If your metric is longer than MaxBytes autoTruncate can
	// be used to truncate the message instead of erroring
	DefaultAutoTruncate = false
)

var printOutput bool

type passengerStatus struct {
	XMLName      xml.Name `xml:"info"`
	ProcessCount int      `xml:"process_count"`
	PoolMax      int      `xml:"max"`
	PoolCurrent  int      `xml:"capacity_used"`
	Groups       []group  `xml:"supergroups>supergroup>group"`
	QueuedCount  []int
	Processes    []process
}

type group struct {
	Name            string    `xml:"name"`
	AppRoot         string    `xml:"app_root"`
	Processes       []process `xml:"processes>process"`
	QueuedCount     []int     `xml:"get_wait_list_size"`
	AppName         string
	PassengerConfig passengerConfig
}

type process struct {
	CurrentSessions int   `xml:"sessions"`
	Processed       int   `xml:"processed"`
	SpawnTime       int64 `xml:"spawn_end_time"`
	CPU             int   `xml:"cpu"`
	Memory          int   `xml:"real_memory"`
	Swap            int   `xml:"swap"`
	PID             int   `xml:"pid"`
	LastUsed        int64 `xml:"last_used"`
	Group           group
	Tags            []string
}

//Stats is used to store stats
type Stats struct {
	min int
	len int
	avg int
	max int
	sum int
}

type passengerConfig struct {
	Root        string
	MemoryLimit int
}

func summarizeStats(statsArray *[]int) Stats {
	var processedStats Stats
	sum, count := 0, len(*statsArray)
	sort.Sort(sort.IntSlice(*statsArray))

	for _, stat := range *statsArray {
		sum += stat
	}
	sortedStats := *statsArray
	processedStats.min = sortedStats[0]
	processedStats.len = count
	processedStats.avg = sum / count
	processedStats.max = sortedStats[len(sortedStats)-1]
	processedStats.sum = sum

	return processedStats
}

func retrievePassengerStats() (io.Reader, error) {
	out, err := exec.Command("passenger-status", "--show=xml").Output()
	if err != nil {
		fmt.Println(err)
		return nil, err
	}

	return bytes.NewReader(out), nil
}

func parsePassengerXML(xmlData *io.Reader, passengerConfigs map[string]passengerConfig) (passengerStatus, error) {
	var ParsedPassengerXML passengerStatus
	dec := xml.NewDecoder(*xmlData)
	dec.CharsetReader = charset.NewReaderLabel
	err := dec.Decode(&ParsedPassengerXML)
	if err != nil {
		return passengerStatus{}, err
	}

	for _, group := range ParsedPassengerXML.Groups {
		group.AppName = strings.Split(group.AppRoot, "/")[2]
		rootPath := strings.Split(group.Name, " ")[0]
		group.PassengerConfig = passengerConfigs[rootPath]

		for _, process := range group.Processes {
			process.Group = group
			process.Tags = processTags(process)
			ParsedPassengerXML.Processes = append(ParsedPassengerXML.Processes, process)
		}
		ParsedPassengerXML.QueuedCount = append(ParsedPassengerXML.QueuedCount, group.QueuedCount...)
	}
	return ParsedPassengerXML, nil
}

func processTags(process process) []string {
	return []string{
		fmt.Sprintf("pid:%d", process.PID),
		fmt.Sprintf("passenger_group:%s", process.Group.Name),
		fmt.Sprintf("passenger_root:%s", process.Group.AppRoot),
		fmt.Sprintf("passenger_app:%s", process.Group.AppName),
	}
}

func retrieveNginxPassengerConfigs() map[string]passengerConfig {
	nginxConfigDirectory := "/etc/nginx/sites-enabled"
	files, err := ioutil.ReadDir(nginxConfigDirectory)
	if err != nil {
		fmt.Println(err)
		return nil
	}

	var passengerConfigs = make(map[string]passengerConfig)
	re_memory_limit := regexp.MustCompile(`\s*passenger_memory_limit\s+(.*);`)
	re_root := regexp.MustCompile(`\s*root\s+(.*);`)
	for _, file := range files {
		fileData, err := ioutil.ReadFile(filepath.Join(nginxConfigDirectory, file.Name()))
		if err != nil {
			return nil
		}

		fileContents := string(fileData)
		passengerConfig := passengerConfig{
			MemoryLimit: StringToInt(indexOrEmpty(re_memory_limit.FindStringSubmatch(fileContents), 1)),
			Root:        indexOrEmpty(re_root.FindStringSubmatch(fileContents), 1),
		}
		passengerConfigs[passengerConfig.Root] = passengerConfig
	}
	return passengerConfigs
}

func indexOrEmpty(value []string, index int) string {
	if len(value) <= index {
		return ""
	}
	return value[index]
}

func floatMyInt(value int) float64 {
	return float64(value)
}

func getProcessThreadCount(pid int) (int, error) {
	//ps -o nlwp --no-heading to get number of lightweight processes for given pid
	out, err := exec.Command("ps", "--no-header", "-o", "nlwp", strconv.Itoa(pid)).Output()
	if err != nil {
		return 0, fmt.Errorf("encountered error issuing command to retrieve"+
			" thread count from pid %d, error: %s", pid, err)
	}
	countString := strings.TrimSpace(string(out))
	count, err := strconv.Atoi(countString)
	if err != nil {
		return 0, fmt.Errorf("encountered error parsing thread count from command return value, err: %s", err)
	}
	return count, nil
}

func processed(passengerDetails *passengerStatus) Stats {
	var processed []int
	processes := passengerDetails.Processes
	for _, processStats := range processes {
		processed = append(processed, processStats.Processed)
	}
	return summarizeStats(&processed)
}

func memory(passengerDetails *passengerStatus) Stats {
	var memory []int
	processes := passengerDetails.Processes
	for _, processStats := range processes {
		memory = append(memory, processStats.Memory)
	}
	return summarizeStats(&memory)
}

// Timestamps from Passenger Status are returned in microseconds (1/1,000,000 second) units
// Golang unix time function only accepts seconds or nano (1/1,000,000,000) seconds
// multiplying by 1000 to get nano from micro
func processUptime(passengerDetails *passengerStatus) Stats {
	var upTimes []int
	processes := passengerDetails.Processes
	for _, processStats := range processes {
		SpawnedNano := time.Unix(0, int64(processStats.SpawnTime*1000))
		uptime := time.Since(SpawnedNano)
		upTimes = append(upTimes, int(uptime.Minutes()))
	}
	return summarizeStats(&upTimes)
}

func processUse(passengerDetails *passengerStatus) int {
	var totalUsed int
	processes := passengerDetails.Processes
	periodStart := time.Now().Add(-(10 * time.Second))
	for _, processStats := range processes {
		lastUsedNano := time.Unix(0, int64(processStats.LastUsed*1000))
		if lastUsedNano.After(periodStart) {
			totalUsed++
		}
	}
	return totalUsed
}

func chartPendingRequest(passengerDetails *passengerStatus, DogStatsD *godspeed.Godspeed) {
	var totalQueued int
	for _, queued := range passengerDetails.QueuedCount {
		totalQueued += queued
	}
	if printOutput {
		fmt.Printf("\n|=====Queue Depth====|\n Queue Depth %d", totalQueued)
	}
	_ = DogStatsD.Gauge("passenger.queue.depth", floatMyInt(totalQueued), nil)
}

func chartPoolUse(passengerDetails *passengerStatus, DogStatsD *godspeed.Godspeed) {
	if printOutput {
		fmt.Printf("\n|=====Pool Usage====|\n Used Pool %d\n Max Pool %d", passengerDetails.ProcessCount, passengerDetails.PoolMax)
	}
	_ = DogStatsD.Gauge("passenger.pool.used", floatMyInt(passengerDetails.ProcessCount), nil)
	_ = DogStatsD.Gauge("passenger.pool.max", floatMyInt(passengerDetails.PoolMax), nil)
}

func chartProcessed(passengerDetails *passengerStatus, DogStatsD *godspeed.Godspeed) {
	stats := processed(passengerDetails)
	if printOutput {
		fmt.Printf("\n|=====Processed====|\n Total processed %d\n Average processed %d\n"+
			" Minimum processed %d\n Maximum processed %d", stats.sum, stats.avg, stats.min, stats.max)
	}
	_ = DogStatsD.Gauge("passenger.processed.total", floatMyInt(stats.sum), nil)
	_ = DogStatsD.Gauge("passenger.processed.avg", floatMyInt(stats.avg), nil)
	_ = DogStatsD.Gauge("passenger.processed.min", floatMyInt(stats.min), nil)
	_ = DogStatsD.Gauge("passenger.processed.max", floatMyInt(stats.max), nil)
}

func chartMemory(passengerDetails *passengerStatus, DogStatsD *godspeed.Godspeed) {
	stats := memory(passengerDetails)
	if printOutput {
		fmt.Printf("\n|=====Memory====|\n Total memory %d\n Average memory %d\n"+
			" Minimum memory %d\n Maximum memory %d", stats.sum/1024, stats.avg/1024, stats.min/1024, stats.max/1024)
	}
	_ = DogStatsD.Gauge("passenger.memory.total", floatMyInt(stats.sum/1024), nil)
	_ = DogStatsD.Gauge("passenger.memory.avg", floatMyInt(stats.avg/1024), nil)
	_ = DogStatsD.Gauge("passenger.memory.min", floatMyInt(stats.min/1024), nil)
	_ = DogStatsD.Gauge("passenger.memory.max", floatMyInt(stats.max/1024), nil)
}

func chartProcessUptime(passengerDetails *passengerStatus, DogStatsD *godspeed.Godspeed) {
	stats := processUptime(passengerDetails)
	if printOutput {
		fmt.Printf("\n|=====Process uptime====|\n Average uptime %d min\n"+
			" Minimum uptime %d min\n Maximum uptime %d min\n", stats.avg, stats.min, stats.max)
	}
	_ = DogStatsD.Gauge("passenger.uptime.avg", floatMyInt(stats.avg), nil)
	_ = DogStatsD.Gauge("passenger.uptime.min", floatMyInt(stats.min), nil)
	_ = DogStatsD.Gauge("passenger.uptime.max", floatMyInt(stats.max), nil)
}

func chartProcessUse(passengerDetails *passengerStatus, DogStatsD *godspeed.Godspeed) {
	totalUsed := processUse(passengerDetails)
	if printOutput {
		fmt.Printf("\n|=====Process Usage====|\nUsed Processes %d", totalUsed)
	}
	_ = DogStatsD.Gauge("passenger.processes.used", floatMyInt(totalUsed), nil)
}

//go through each process in the tree and match their group
func processesByPid(passengerDetails *passengerStatus) map[int]process {
	processesByPid := make(map[int]process)
	for _, process := range passengerDetails.Processes {
		processesByPid[process.PID] = process
	}
	return processesByPid
}

//go through each process in the tree and get the per process thread count and per process last used time
func processSystemThreadUsage(passengerDetails *passengerStatus) map[int]float64 {
	var processThreads = make(map[int]float64)
	p := passengerDetails.Processes
	for _, processDetails := range p {
		//take the PID and do a thread count lookup using PS
		tc, err := getProcessThreadCount(processDetails.PID)
		if err != nil {
			_ = err
			//log.Printf("encountered error getting thread count %s", err)
		}
		processThreads[processDetails.PID] = floatMyInt(tc)
	}
	return processThreads
}

func processPerThreadMemoryUsage(passengerDetails *passengerStatus) map[int]float64 {
	var processMemory = make(map[int]float64)
	p := passengerDetails.Processes
	for _, processDetails := range p {
		processMemory[processDetails.PID] = floatMyInt(processDetails.Memory) / 1024
	}
	return processMemory
}

// Timestamps from Passenger Status are returned in microseconds units
// Golang unix time function only accepts seconds or nano seconds
// multiplying by 1000 to get nano from micro
func processPerThreadIdleTime(passengerDetails *passengerStatus) map[int]float64 {
	var processIdleTimes = make(map[int]float64)
	p := passengerDetails.Processes
	for _, processDetails := range p {
		LastUsedTime := time.Unix(0, int64(processDetails.LastUsed*1000))
		lastUsedSeconds := time.Since(LastUsedTime).Seconds()
		processIdleTimes[processDetails.PID] = lastUsedSeconds
	}
	return processIdleTimes
}

func processPerThreadRequests(passengerDetails *passengerStatus) map[int]float64 {
	var processPerThreadProcessed = make(map[int]float64)
	p := passengerDetails.Processes
	for _, processedReq := range p {
		processPerThreadProcessed[processedReq.PID] = floatMyInt(processedReq.Processed)
	}
	return processPerThreadProcessed
}

func chartDiscreteMetrics(passengerDetails *passengerStatus, DogStatsD *godspeed.Godspeed) {
	processesByPid := processesByPid(passengerDetails)
	threadCountPerProcess := processSystemThreadUsage(passengerDetails)
	threadMemoryUsages := processPerThreadMemoryUsage(passengerDetails)
	threadIdletimes := processPerThreadIdleTime(passengerDetails)
	threadProcessedCounts := processPerThreadRequests(passengerDetails)

	if printOutput {
		fmt.Println("\n|====Process Stats====|")
	}
	for index, process := range passengerDetails.Processes {
		memoryLimit := floatMyInt(process.Group.PassengerConfig.MemoryLimit)
		cpuUsage := floatMyInt(process.CPU)
		swapUsage := floatMyInt(process.Swap)
		if printOutput {
			if index > 0 {
				fmt.Println("----")
			}
			fmt.Printf("PID: %d\nMemory_Limit: %0.2f MB\n", process.PID, memoryLimit)
			fmt.Printf("Memory_Limit: %v\n", memoryLimit)
			fmt.Printf("CPU: %0.2f\n", cpuUsage)
			fmt.Printf("Swap: %0.2f\n", swapUsage)
		}
		_ = DogStatsD.Gauge("passenger.process.memory_limit", memoryLimit, process.Tags)
		_ = DogStatsD.Gauge("passenger.process.cpu", cpuUsage, process.Tags)
		_ = DogStatsD.Gauge("passenger.process.swap", swapUsage, process.Tags)
	}

	if printOutput {
		fmt.Println("\n|====Process Thread Counts====|")
	}
	for pid, count := range threadCountPerProcess {
		if printOutput {
			fmt.Printf("PID: %d  Running: %0.2f threads\n", pid, count)
		}
		process := processesByPid[pid]
		_ = DogStatsD.Gauge("passenger.process.threads", count, process.Tags)
	}

	if printOutput {
		fmt.Println("|====Process Memory Usage====|")
	}
	for pid, memUse := range threadMemoryUsages {
		if printOutput {
			fmt.Printf("PID: %d Memory_Used: %0.2f MB\n", pid, memUse)
		}
		process := processesByPid[pid]
		_ = DogStatsD.Gauge("passenger.process.memory", memUse, process.Tags)
	}

	if printOutput {
		fmt.Println("|====Process Idle Times====|")
	}
	for pid, seconds := range threadIdletimes {
		if printOutput {
			fmt.Printf("PID: %d Idle: %d Seconds\n", pid, int(seconds))
		}
		process := processesByPid[pid]
		_ = DogStatsD.Gauge("passenger.process.last_used", seconds, process.Tags)
	}

	if printOutput {
		fmt.Println("|====Process Requests Handled====|")
	}
	for pid, count := range threadProcessedCounts {
		if printOutput {
			fmt.Printf("PID: %d Processed: %d Requests\n", pid, int(count))
		}
		process := processesByPid[pid]
		_ = DogStatsD.Gauge("passenger.process.request_processed", count, process.Tags)
	}
}

func StringToInt(value string) int {
	intValue, err := strconv.Atoi(value)
	if err != nil {
		return -1
	}
	return intValue
}

func main() {
	//get host and port
	hostName := flag.String("host", DefaultHost, "DogStatsD Host")
	portNum := flag.Int("port", DefaultPort, "DogStatsD UDP Port")
	flag.BoolVar(&printOutput, "print", false, "Print Outputs")

	flag.Parse()

	//backwards compatibility
	if flag.NArg() > 0 && flag.Arg(0) == "print" {
		printOutput = true
	}

	if printOutput {
		log.Println("Starting loop, sending to", *hostName, *portNum)
	}

	for {
		xmlData, err := retrievePassengerStats()
		if err != nil {
			log.Fatal("Error getting passenger data:", err)
		}

		passengerConfigs := retrieveNginxPassengerConfigs()
		PassengerStatusData, err := parsePassengerXML(&xmlData, passengerConfigs)
		if err != nil {
			log.Fatal("Error parsing passenger data:", err)
		}

		if PassengerStatusData.ProcessCount == 0 {
			log.Println("Passenger has not yet started any threads, will try again next loop")
		} else {
			DogStatsD, err := godspeed.New(*hostName, *portNum, DefaultAutoTruncate)
			if err != nil {
				log.Fatal("Error establishing StatsD connection", err)
			}

			chartProcessed(&PassengerStatusData, DogStatsD)
			chartMemory(&PassengerStatusData, DogStatsD)
			chartPendingRequest(&PassengerStatusData, DogStatsD)
			chartPoolUse(&PassengerStatusData, DogStatsD)
			chartProcessUptime(&PassengerStatusData, DogStatsD)
			chartProcessUse(&PassengerStatusData, DogStatsD)
			chartDiscreteMetrics(&PassengerStatusData, DogStatsD)

			DogStatsD.Conn.Close()
		}

		time.Sleep(10 * time.Second)
	}
}
