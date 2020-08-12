package main

import (
	"bufio"
	"bytes"
	"fmt"
	"github.com/Yprolic/TomlConfiguration"
	"io"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

//Config 全局配置类
type Config struct {
	Input    string
	ErrorLog string
	User     int
	Timeout  int
	Url      string
}

//Conf 全局配置
var (
	Conf    *Config
	logger  *log.Logger
	logFile *os.File
)

func init() {
	m := TomlConfiguration.TOMLLoader{Path: "conf.toml"}
	Conf = &Config{}
	if err := m.Load(Conf); err != nil {
		panic("config load error")
	}
	logFile, _ = os.Create(Conf.ErrorLog)
	logger = log.New(logFile, "", 0)
}

var ch = make(chan string, 10000)
var done = make(chan int, 1E6)
var d = make(chan []int, 1E6)

func counter() {
	t := time.NewTicker(time.Second)

	for {
	LOOP:
		var v []int
		for {
			select {
			case latency := <-done:
				v = append(v, latency)
			case <-t.C:
				d <- v
				goto LOOP
			}
		}
	}
}

func p(wg *sync.WaitGroup) {
	for t := range d {
		qps := len(t)
		if qps == 0 {
			break
		}
		sort.Ints(t)
		pos1 := int(math.Round(float64(qps)*0.95) - 1)
		pos2 := int(math.Round(float64(qps)*0.99) - 1)
		errCount := 0
		for i := 0; i < qps; i++ {
			if int(Conf.Timeout*1000000) <= t[i] {
				errCount ++
			}
		}
		fmt.Printf("QPS %d P95 %.2f ms P99 %.2f ms Timeout %.2f %% \n", qps, float32(t[pos1])/1000000, float32(t[pos2])/1000000, (float32(errCount)/float32(qps))*100)
	}
	wg.Done()
}

func worker(wg *sync.WaitGroup) {
	client := http.Client{}
	t := http.Transport{}
	t.DisableKeepAlives = false
	client.Transport = &t
	client.Timeout = time.Duration(Conf.Timeout) * time.Millisecond

	for q := range ch {
		n := time.Now()
		jsonStr := []byte(q)
		req, _ := http.NewRequest("POST", Conf.Url, bytes.NewBuffer(jsonStr))
		resp, err := client.Do(req)

		if err != nil {
			logger.Print(q)
			logger.Print(err)
			done <- int(Conf.Timeout * 1000000)
		} else {
			ioutil.ReadAll(resp.Body)
			resp.Body.Close()
			done <- int(time.Now().Sub(n).Nanoseconds())
		}

	}
	wg.Done()
}

func main() {
	f, _ := os.Open(Conf.Input)
	r := bufio.NewReader(f)
	var lines []string
	for {
		l, err := r.ReadBytes('\n')
		if err == io.EOF {
			break
		} else {
			lines = append(lines, strings.TrimSpace(string(l)))
		}
	}
	go counter()
	var wg sync.WaitGroup
	wg.Add(1)
	go p(&wg)

	for i := 0; i < Conf.User; i++ {
		wg.Add(1)
		go worker(&wg)
	}
	for _, q := range lines {
		ch <- string(q)
	}
	close(ch)
	wg.Wait()
	logFile.Close()
}
