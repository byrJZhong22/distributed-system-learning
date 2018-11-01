package mapreduce

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

type StringSlice []string

func (s StringSlice) Len() int {
	return len(s)
}

func (s StringSlice) Less(i, j int) bool {
	return strings.Compare(s[i], s[j]) == -1
}

func (s StringSlice) Swap(i, j int) {
	s[i], s[j] = s[j], s[i]
}

// doReduce manages one reduce task: it reads the intermediate
// key/value pairs (produced by the map phase) for this task, sorts the
// intermediate key/value pairs by key, calls the user-defined reduce function
// (reduceF) for each key, and writes the output to disk.
func doReduce(
	jobName string, // the name of the whole MapReduce job
	reduceTaskNumber int, // which reduce task this is
	outFile string, // write the output here
	nMap int, // the number of map tasks that were run ("M" in the paper)
	reduceF func(key string, values []string) string,
) {
	//
	// You will need to write this function.
	//
	// You'll need to read one intermediate file from each map task;
	// reduceName(jobName, m, reduceTaskNumber) yields the file
	// name from map task m.
	//
	// Your doMap() encoded the key/value pairs in the intermediate
	// files, so you will need to decode them. If you used JSON, you can
	// read and decode by creating a decoder and repeatedly calling
	// .Decode(&kv) on it until it returns an error.
	//
	// You may find the first example in the golang sort package
	// documentation useful.
	//
	// reduceF() is the application's reduce function. You should
	// call it once per distinct key, with a slice of all the values
	// for that key. reduceF() returns the reduced value for that key.
	//
	// You should write the reduce output as JSON encoded KeyValue
	// objects to the file named outFile. We require you to use JSON
	// because that is what the merger than combines the output
	// from all the reduce tasks expects. There is nothing special about
	// JSON -- it is just the marshalling format we chose to use. Your
	// output code will look something like this:
	//
	// enc := json.NewEncoder(file)
	// for key := ... {
	// 	enc.Encode(KeyValue{key, reduceF(...)})
	// }
	// file.Close()
	//

	interMediateFiles := make([]*os.File, nMap)
	decoders := make([]*json.Decoder, nMap)
	for i := 0; i < nMap; i++ {
		interMediateFileName := reduceName(jobName, i, reduceTaskNumber)
		interMediateFile, err := os.OpenFile(interMediateFileName, os.O_RDONLY, 0666)
		if err != nil {
			fmt.Printf("Failed to open REDUCE input file %s: %v\n", interMediateFileName, err)
		}
		defer interMediateFile.Close()
		interMediateFiles[i] = interMediateFile
		decoders[i] = json.NewDecoder(interMediateFile)
	}

	inKVs := make(map[string][]string)
	for i := 0; i < nMap; i++ {
		for {
			var kv KeyValue
			err := decoders[i].Decode(&kv)
			if err != nil {
				if err.Error() != "EOF" {
					fmt.Printf("Failed to read REDUCE input file %s: %v\n", interMediateFiles[i].Name(), err)
				}
				break
			}
			inKVs[kv.Key] = append(inKVs[kv.Key], kv.Value)
		}
	}

	keys := make([]string, len(inKVs))
	var i = 0
	for k := range inKVs {
		keys[i] = k
		i++
	}
	sort.Sort(StringSlice(keys))

	out, err := os.OpenFile(outFile, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		fmt.Printf("Failed to create REDUCE output file %s: %vn", outFile, err)
		return
	}
	defer out.Close()
	enc := json.NewEncoder(out)

	for _, key := range keys {
		values := inKVs[key]
		reduced := reduceF(key, values)
		enc.Encode(KeyValue{key, reduced})
	}

}
