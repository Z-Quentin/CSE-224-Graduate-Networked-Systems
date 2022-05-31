package main

import (
	"io"
	"io/ioutil"
	"log"
	"math"
	"net"
	"os"
	"sort"
	"strconv"
	"time"
	"fmt"

	"gopkg.in/yaml.v2"
)

type ServerConfigs struct {
	Servers []struct {
		ServerId int    `yaml:"serverId"`
		Host     string `yaml:"host"`
		Port     string `yaml:"port"`
	} `yaml:"servers"`
}

type Client struct {
	Conn   net.Conn
	Record []byte
}

func readServerConfigs(configPath string) ServerConfigs {
	f, err := ioutil.ReadFile(configPath)

	if err != nil {
		log.Fatalf("could not read config file %s : %v", configPath, err)
	}

	scs := ServerConfigs{}
	_ = yaml.Unmarshal(f, &scs)

	return scs
}

func handleConnection(conn net.Conn, ch chan<- Client) {
	record := make([]byte, 100)
	n, err := conn.Read(record)
	if err != nil {
		log.Print("Could not read bytes: ", err)
	}
	record = record[:n]
	ch <- Client{conn, record}
}

func listenForRecords(ch chan<- Client, host string, port string) {
	l, err := net.Listen("tcp", host+":"+port)
	if err != nil {
		log.Println("Could not listen on network: ", err)
	}
	defer l.Close()

	for {
		conn, err := l.Accept()
		if err != nil {
			log.Println("Could not accept connection: ", err)
		}
		go handleConnection(conn, ch)
	}
}

func callTransferRecord(host string, port string, record []byte) {
	var conn net.Conn
	var err error
	for i := 0; i < 5; i++ {
		conn, err = net.Dial("tcp", host+":"+port)
		if err != nil {
			log.Println("Could not dial: ", err)
			time.Sleep(50 * time.Millisecond)
		} else {
			break
		}
	}
	defer conn.Close()

	_, err = conn.Write(record)
	if err != nil {
		log.Println("Could not write: ", err)
	}
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	if len(os.Args) != 5 {
		log.Fatal("Usage : ./netsort {serverId} {inputFilePath} {outputFilePath} {configFilePath}")
	}

	// What is my serverId
	serverId, err := strconv.Atoi(os.Args[1])
	if err != nil {
		log.Fatalf("Invalid serverId, must be an int %v", err)
	}
	fmt.Println("My server Id:", serverId)

	// Read server configs from file
	scs := readServerConfigs(os.Args[4])
	fmt.Println("Got the following server configs:", scs)

	// Set up as server
	ch := make(chan Client)
	for _, server := range scs.Servers {
		if serverId == server.ServerId {
			go listenForRecords(ch, server.Host, server.Port)
			break
		}
	}

	time.Sleep(200 * time.Millisecond)

	readPath := os.Args[2]
	readFile, err := os.Open(readPath)
	if err != nil {
		log.Println("Error opening file: ", err)
	}

	numServers := len(scs.Servers)
	partitioningBits := int(math.Log2(float64(numServers))) //int is an allowed assumprion
	for {
		record := make([]byte, 100)
		_, err := readFile.Read(record)
		if err != nil {
			if err == io.EOF {
				break
			}
			log.Println("Error reading file: ", err)
		}
		//record = record[:n] // in case format error on last value, necessary??

		respServerId := int(record[0] >> (8 - partitioningBits))
		for _, server := range scs.Servers {
			if respServerId == server.ServerId {
				callTransferRecord(server.Host, server.Port, record)
				break
			}
		}
	}
	readFile.Close()

	//signal done to other servers
	signalDone := []byte{}
	for i := 0; i < 100; i++ {
		signalDone = append(signalDone, 0)
	}
	for _, server := range scs.Servers {
		callTransferRecord(server.Host, server.Port, signalDone)
	}

	//read own records
	recordArray := [][]byte{}
	numCompleted := 0
	for {
		if numCompleted == numServers {
			break
		}
		msg := <-ch
		allZero := true
		for _, checkByte := range msg.Record {
			if checkByte != 0 {
				allZero = false
				break
			}
		}

		if allZero {
			numCompleted += 1
		} else {
			recordArray = append(recordArray, msg.Record)
		}
	}

	sort.Slice(recordArray, func(i, j int) bool { return string(recordArray[i][:10]) < string(recordArray[j][:10]) })

	writePath := os.Args[3]
	writeFile, err := os.Create(writePath)
	if err != nil {
		log.Println("Error opening writefile: ", err)
	}

	for _, record := range recordArray {
		writeFile.Write(record)
	}

	err = writeFile.Close()
	if err != nil {
		log.Println("Error closing writefile: ", err)
	}
}
