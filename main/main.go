package main

//go:generate go run v2ray.com/core/common/errors/errorgen

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"

	"v2ray.com/core"
	"v2ray.com/core/common/cmdarg"
	"v2ray.com/core/common/platform"
	_ "v2ray.com/core/main/distro/all"
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

	mu        sync.RWMutex
	mapConfig map[string]core.Server
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

		input := bytes.NewReader(b)
		startNewV2Ray(string(b), input, w)
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
		mu.Lock()
		defer mu.Unlock()
		if c, find := mapConfig[string(b)]; find {
			c.Close()
			// Explicitly triggering GC to remove garbage from config loading.
			//runtime.GC()
			delete(mapConfig, string(b))
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
		mu.RLock()
		defer mu.RUnlock()
		for k := range mapConfig {
			cfgs = append(cfgs, k)
		}

		fmt.Fprintf(w, "%v", cfgs)
	} else {
		log.Println("ONly support Post")
		fmt.Fprintf(w, "Only support post")
	}
}

func startNewV2Ray(key string, input io.Reader, w http.ResponseWriter) {
	mu.Lock()
	if c, find := mapConfig[key]; find {
		log.Println("已经有了同样一个实例")
		fmt.Fprintf(w, "已经有了同样一个实例")
		c.Close()
		// Explicitly triggering GC to remove garbage from config loading.
		//runtime.GC()
		delete(mapConfig, key)
	}

	server, err := startV2RayCoustom(input)
	if err != nil {
		fmt.Println(err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	mapConfig[key] = server
	/*if *test {*/
	//fmt.Println("Configuration OK.")
	//return
	/*}*/

	mu.Unlock()
	fmt.Fprintf(w, "succ")
	go func() {
		if err := server.Start(); err != nil {
			mu.Lock()
			delete(mapConfig, key)
			mu.Unlock()
			fmt.Println("Failed to start", err)
			fmt.Fprintln(w, "Failed to start")
			http.Error(w, "Failed to start", http.StatusInternalServerError)
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

	mapConfig = map[string]core.Server{}

	http.HandleFunc("/add", addCfg)
	http.HandleFunc("/delete", deleteCfg)
	http.HandleFunc("/get", getCfg)
	http.ListenAndServe(":6543", nil)

	// XXX 下面的代码先注释起来, 系统稳定后删除 2020.11.4
	server, err := startV2Ray()
	if err != nil {
		fmt.Println(err)
		// Configuration error. Exit with a special value to prevent systemd from restarting.
		os.Exit(23)
	}

	if *test {
		fmt.Println("Configuration OK.")
		os.Exit(0)
	}

	if err := server.Start(); err != nil {
		fmt.Println("Failed to start", err)
		os.Exit(-1)
	}
	defer server.Close()

	// Explicitly triggering GC to remove garbage from config loading.
	runtime.GC()

	{
		osSignals := make(chan os.Signal, 1)
		signal.Notify(osSignals, os.Interrupt, syscall.SIGTERM)
		<-osSignals
	}
}
