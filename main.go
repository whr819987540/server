package main

import (
	"config"
	"flag"
	"fmt"
	"io/ioutil"
	"net/http"
	"path"
	"strconv"
	"sync"
	"time"

	"github.com/anacrolix/log"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/anacrolix/torrent/storage"
)

var debugFlag *bool

var data []byte
var startTime time.Time
var sendTimes int // 完成发送的次数
var recvTimes int // 完成接收的次数
var mutex sync.Mutex
var torrentURL string
var mi *metainfo.MetaInfo
var configStruct *config.Config

func accessLog(r *http.Request) {
	log.Printf("%s %s from %s", r.Method, r.RequestURI, r.RemoteAddr)
}

func handleStatus(w http.ResponseWriter, r *http.Request) {
	accessLog(r)
	w.Write([]byte("hello"))
}

// client向server请求发送数据
func handleSend(w http.ResponseWriter, r *http.Request) {
	log.Printf("%s %s from %s", r.Method, r.RequestURI, r.RemoteAddr)
	var err error
	// 从收到第一个send开始计时
	if startTime.IsZero() {
		startTime = time.Now()
		log.Print("start timer")
	}

	// 还没有生成.torrent
	// create torrent from memory and seed
	if mi == nil {
		mi, err = fromMemory(data)
		info, err := infoBytesToInfo(mi.InfoBytes)
		if err != nil {
			log.Printf("infoBytesToInfo: %v", err)
		}
		totalLength := info.TotalLength()
		log.Printf("TotalLength %d\n", totalLength)

		mb := &storage.MemoryBuf{
			Data:   data,
			Length: totalLength,
		}
		seed(mi, mb)
	}

	err = mi.Write(w)
	if err != nil {
		log.Printf("send .torrent to %s error:%v", r.RemoteAddr, err)
		return
	}
	log.Printf("send .torrent to %s ok", r.RemoteAddr)
}

// client向server回传数据
func handleRecv(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	log.Printf("%s %s from %s", r.Method, r.RequestURI, r.RemoteAddr)
	data, err := ioutil.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Error reading request body", http.StatusInternalServerError)
		return
	}
	fmt.Fprintf(w, "OK") // POST请求需要有回复
	log.Printf("recv %d bytes from %s", len(data), r.RemoteAddr)

	mutex.Lock()
	defer mutex.Unlock()
	recvTimes++
	if recvTimes == configStruct.Client.TotalPeers {
		endTime := time.Now()
		log.Printf("PS, %s, total time: %v", configStruct.Model.ModelName, endTime.Sub(startTime))
	}
}

// client完成接收后, 通知server
func handleCompleteSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	data, err := ioutil.ReadAll(r.Body)
	defer r.Body.Close()
	if err != nil {
		http.Error(w, "Error reading request body", http.StatusInternalServerError)
		return
	}
	w.Write([]byte("ok")) // POST请求需要有回复

	log.Printf("recv send status from %s,%s", r.RemoteAddr, string(data))
	mutex.Lock()
	defer mutex.Unlock()
	sendTimes++
	log.Printf("%s add sendTimes to %d", r.RemoteAddr, sendTimes)
}

// 获取完成向client发送的次数
func handleGetSendTimes(w http.ResponseWriter, r *http.Request) {
	mutex.Lock()
	defer mutex.Unlock()
	w.Write([]byte(strconv.Itoa(sendTimes)))
}

func httpFunc() {
	http.HandleFunc("/status/", handleStatus)
	http.HandleFunc("/send/", handleSend)
	http.HandleFunc("/recv/", handleRecv)
	http.HandleFunc("/completesend/", handleCompleteSend)
	http.HandleFunc("/sendtimes/", handleGetSendTimes)

	if err := http.ListenAndServe(fmt.Sprintf(":%d", configStruct.Port.HTTPPort), nil); err != nil {
		log.Printf("listen %d error", configStruct.Port.HTTPPort)
	}
}

func main() {
	var err error

	debugFlag = flag.Bool("debug", false, "debug flag")
	flag.Parse()

	// 加载配置数据
	jsoncFileName := "config.jsonc"
	configStruct, err = config.LoadJsonc(jsoncFileName)
	if err != nil {
		fmt.Errorf("%v", err)
	}

	// 读取模型数据
	modleParamPath := path.Join(configStruct.Model.ModelPath, configStruct.Model.ModelName)
	data, err = readModelParam(modleParamPath)
	if err != nil {
		log.Printf("read %s param error: %v", configStruct.Model.ModelName, err)
	}
	log.Printf("read %d bytes from model %s", len(data), configStruct.Model.ModelName)

	httpFunc()
}
