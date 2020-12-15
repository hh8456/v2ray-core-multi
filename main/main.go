package main

//go:generate go run v2ray.com/core/common/errors/errorgen

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/natefinch/lumberjack.v2"

	"v2ray.com/core"
	"v2ray.com/core/common/cmdarg"
	"v2ray.com/core/common/platform"
	"v2ray.com/core/infra/conf/serial"
	_ "v2ray.com/core/main/distro/all"
	"v2ray.com/core/main/wlogrus"
)

var (
	configFiles cmdarg.Arg // "Config file for V2Ray.", the option is customed type, parse in main
	configDir   string
	version     = flag.Bool("version", false, "Show current version of V2Ray.")
	test        = flag.Bool("test", false, "Test config file only, without launching V2Ray server.")
	format      = flag.String("format", "json", "Format of input file.")

	/* We have to do this here because Golang's Test will also need to parse flag, before
	 * main func in this file is run.
	 */
	_ = func() error {

		flag.Var(&configFiles, "config", "Config file for V2Ray. Multiple assign is accepted (only json). Latter ones overrides the former ones.")
		flag.Var(&configFiles, "c", "Short alias of -config")
		flag.StringVar(&configDir, "confdir", "", "A dir with multiple json config")

		return nil
	}()

	//mapConfig map[string]core.Server
	mapConfig sync.Map // config - core.Server
)

func md5_32bit(s string) string {
	h := md5.New()
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}

func fileExists(file string) bool {
	info, err := os.Stat(file)
	return err == nil && !info.IsDir()
}

func dirExists(file string) bool {
	if file == "" {
		return false
	}
	info, err := os.Stat(file)
	return err == nil && info.IsDir()
}

func readConfDir(dirPath string) {
	confs, err := ioutil.ReadDir(dirPath)
	if err != nil {
		log.Fatalln(err)
	}
	for _, f := range confs {
		if strings.HasSuffix(f.Name(), ".json") {
			configFiles.Set(path.Join(dirPath, f.Name()))
		}
	}
}

func getConfigFilePath() (cmdarg.Arg, error) {
	if dirExists(configDir) {
		log.Println("Using confdir from arg:", configDir)
		readConfDir(configDir)
	} else if envConfDir := platform.GetConfDirPath(); dirExists(envConfDir) {
		log.Println("Using confdir from env:", envConfDir)
		readConfDir(envConfDir)
	}

	if len(configFiles) > 0 {
		return configFiles, nil
	}

	if workingDir, err := os.Getwd(); err == nil {
		configFile := filepath.Join(workingDir, "config.json")
		if fileExists(configFile) {
			log.Println("Using default config: ", configFile)
			return cmdarg.Arg{configFile}, nil
		}
	}

	if configFile := platform.GetConfigurationPath(); fileExists(configFile) {
		log.Println("Using config from env: ", configFile)
		return cmdarg.Arg{configFile}, nil
	}

	log.Println("Using config from STDIN")
	return cmdarg.Arg{"stdin:"}, nil
}

func GetConfigFormat() string {
	switch strings.ToLower(*format) {
	case "pb", "protobuf":
		return "protobuf"
	default:
		return "json"
	}
}

func fileIsExisted(filename string) bool {
	existed := true
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		existed = false
	}
	return existed
}

func startV2RayCoustom(input io.Reader) (core.Server, error) {
	config, err := core.LoadConfig("json", "", input)
	if err != nil {
		return nil, newError("failed to read config files: [", configFiles.String(), "]").Base(err)
	}

	server, err := core.New(config)
	if err != nil {
		return nil, newError("failed to create server").Base(err)
	}

	return server, nil
}

func startV2Ray() (core.Server, error) {
	configFiles, err := getConfigFilePath()
	if err != nil {
		return nil, err
	}

	fmt.Printf("configFiles = %v\n", configFiles)

	config, err := core.LoadConfig(GetConfigFormat(), configFiles[0], configFiles)
	if err != nil {
		return nil, newError("failed to read config files: [", configFiles.String(), "]").Base(err)
	}

	server, err := core.New(config)
	if err != nil {
		return nil, newError("failed to create server").Base(err)
	}

	return server, nil
}

func printVersion() {
	version := core.VersionStatement()
	for _, s := range version {
		fmt.Println(s)
	}
}

func addCfg(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		b, err := ioutil.ReadAll(r.Body)
		if err != nil {
			log.Println("Read failed:", err)
		}
		defer r.Body.Close()

		trimStr := strings.TrimSpace(string(b))
		input := bytes.NewReader([]byte(trimStr))
		startNewV2Ray(trimStr, input, w)
	} else {
		log.Println("ONly support Post")
		fmt.Fprintf(w, "Only support post")
	}
}

func deleteCfg(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		b, err := ioutil.ReadAll(r.Body)
		if err != nil {
			log.Println("Read failed:", err)
		}
		defer r.Body.Close()
		if v, loaded := mapConfig.Load(string(b)); loaded {
			// Explicitly triggering GC to remove garbage from config loading.
			//runtime.GC()
			mapConfig.Delete(string(b))
			jsonConfig, err := serial.DecodeJSONConfig(bytes.NewReader(b))
			if err == nil {
				for _, v := range jsonConfig.InboundConfigs {
					if v.Tag == "proxy" {
						if v.PortRange != nil {
							wlogrus.Debugf("释放了一个代理实例, 端口号: from - %d, to - %d", v.PortRange.From, v.PortRange.To)
						}
					}
				}
			}

			v.(core.Server).Close()
			fmt.Fprintf(w, "删除了一个实例")
			log.Println("删除了一个实例")
			return
		}

		fmt.Fprintf(w, "删除时, 没有发现对应的配置")
		log.Println("删除时, 没有发现对应的配置")
	} else {
		log.Println("ONly support Post")
		fmt.Fprintf(w, "Only support post")
	}
}

func getCfg(w http.ResponseWriter, r *http.Request) {
	if r.Method == "POST" {
		cfgs := make([]string, 0, 10)
		mapConfig.Range(func(k, v interface{}) bool {
			cfgs = append(cfgs, k.(string))
			return true
		})

		fmt.Fprintf(w, "%v", cfgs)
	} else {
		log.Println("ONly support Post")
		fmt.Fprintf(w, "Only support post")
	}
}

func usingport(w http.ResponseWriter, r *http.Request) {
	type Usingport struct {
		From int
		To   int
	}

	type UsingportList struct {
		UsingPorts []*Usingport
	}

	ret := UsingportList{}
	mapConfig.Range(func(k, v interface{}) bool {
		jsonConfig, err := serial.DecodeJSONConfig(bytes.NewReader([]byte(k.(string))))
		if err == nil {
			for _, v := range jsonConfig.InboundConfigs {
				if v.Tag == "proxy" {
					if v.PortRange != nil {
						up := &Usingport{}
						up.From = int(v.PortRange.From)
						up.To = int(v.PortRange.To)
						ret.UsingPorts = append(ret.UsingPorts, up)
					}
				}
			}
		}

		return true
	})

	js, _ := json.Marshal(&ret)
	fmt.Fprintf(w, "%s", js)

}

func startNewV2Ray(key string, input io.Reader, w http.ResponseWriter) {
	if v, loaded := mapConfig.Load(key); loaded {
		log.Println("已经有了同样一个实例")
		fmt.Fprintf(w, "已经有了同样一个实例")
		v.(core.Server).Close()
		// Explicitly triggering GC to remove garbage from config loading.
		//runtime.GC()
		mapConfig.Delete(key)
	}
	server, err := startV2RayCoustom(input)
	if err != nil {
		fmt.Println(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	mapConfig.Store(key, server)

	jsonConfig, err := serial.DecodeJSONConfig(bytes.NewReader([]byte(key)))
	if err == nil {
		for _, v := range jsonConfig.InboundConfigs {
			if v.Tag == "proxy" {
				if v.PortRange != nil {
					wlogrus.Debugf("增加了一个代理实例, 监听本机端口: from - %d, to - %d", v.PortRange.From, v.PortRange.To)
				}
			}
		}
	}

	fmt.Fprintf(w, "succ")
	go func() {
		if err := server.Start(); err != nil {
			mapConfig.Delete(key)
			fmt.Println("Failed to start", err)
			fmt.Fprintln(w, "Failed to start")
			http.Error(w, "Failed to start", http.StatusInternalServerError)
			jsonConfig, err := serial.DecodeJSONConfig(bytes.NewReader([]byte(key)))
			if err == nil {
				for _, v := range jsonConfig.InboundConfigs {
					if v.Tag == "proxy" {
						if v.PortRange != nil {
							wlogrus.Debugf("释放了一个代理实例, 释放本机端口: from - %d, to - %d", v.PortRange.From, v.PortRange.To)
						}
					}
				}
			}

			return
		}

	}()
}

func main() {
	flag.Parse()

	printVersion()

	if *version {
		return
	}

	logger := &lumberjack.Logger{
		Filename:   "./v2ray.log",
		MaxSize:    1024,
		MaxBackups: 100,
		MaxAge:     28,
	}

	writers := []io.Writer{
		logger,
		os.Stdout,
	}

	fileAndStdoutWriter := io.MultiWriter(writers...)

	wlogrus.SetOutput(fileAndStdoutWriter)
	wlogrus.SetLevel(wlogrus.TraceLevel)

	//mapConfig = map[string]core.Server{}

	http.HandleFunc("/add", addCfg)
	http.HandleFunc("/delete", deleteCfg)
	http.HandleFunc("/get", getCfg)
	http.HandleFunc("/usingport", usingport)
	http.ListenAndServe(":6543", nil)

	// XXX 下面的代码先注释起来, 系统稳定后删除 2020.11.4
	/*server, err := startV2Ray()*/
	//if err != nil {
	//fmt.Println(err)
	//// Configuration error. Exit with a special value to prevent systemd from restarting.
	//os.Exit(23)
	//}

	//if *test {
	//fmt.Println("Configuration OK.")
	//os.Exit(0)
	//}

	//if err := server.Start(); err != nil {
	//fmt.Println("Failed to start", err)
	//os.Exit(-1)
	//}
	//defer server.Close()

	//// Explicitly triggering GC to remove garbage from config loading.
	//runtime.GC()

	//{
	//osSignals := make(chan os.Signal, 1)
	//signal.Notify(osSignals, os.Interrupt, syscall.SIGTERM)
	//<-osSignals
	/*}*/
}
