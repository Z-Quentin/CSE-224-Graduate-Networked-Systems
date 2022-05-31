package main

import (
	"io"
	"log"
	"os"
	"sort"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	if len(os.Args) != 3 {
		log.Fatalf("Usage: %v inputfile outputfile\n", os.Args[0])
	}
	readPath := os.Args[1]
	
	readFile, err := os.Open(readPath)
	if err != nil {
		log.Println("Error opening file: ", err)
	}
	
	log.Printf("Sorting %s to %s\n", os.Args[1], os.Args[2])
	
	recordArray := [][]byte{}
	
	for {
		record := make([]byte, 100) //unisgned int??
		n, err := readFile.Read(record);
		if err != nil {
			if err == io.EOF{
				break
			}
			log.Println("Error reading file: ", err)
		}
		record = record[:n] // in case format error on last value, necessary??
		recordArray = append(recordArray, record)
	}
	
	readFile.Close()
	sort.Slice(recordArray, func(i, j int) bool {return string(recordArray[i][:10]) < string(recordArray[j][:10])}) 
	
	writePath := os.Args[2]
	writeFile, err := os.Create(writePath)
	if err != nil {
		log.Println("Error opening writefile: ", err)
	}

	for _, record := range recordArray {
		writeFile.Write(record)
	}
	writeFile.Close()
}