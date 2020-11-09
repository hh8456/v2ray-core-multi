package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
)

type Cmd struct {
	ReqType  int
	FileName string
}

var (
	jsCfg = `{
  "policy": null,
  "log": {
    "access": "",
    "error": "",
    "loglevel": "warning"
  },
  "inbounds": [
    {
      "tag": "proxy",
      "port": 1080,
      "listen": "127.0.0.1",
      "protocol": "socks",
      "sniffing": {
        "enabled": true,
        "destOverride": [
          "http",
          "tls"
        ]
      },
      "settings": {
        "auth": "noauth",
        "udp": true,
        "ip": null,
        "address": null,
        "clients": null,
        "decryption": null
      },
      "streamSettings": null
    }
  ],
  "outbounds": [
    {
      "tag": "proxy",
      "protocol": "vmess",
      "settings": {
        "vnext": [
          {
            "address": "guangdong04.vq1021.site",
            "port": 13197,
            "users": [
              {
                "id": "bba882df-2adf-3521-89de-3a54edac89b1",
                "alterId": 1,
                "email": "t@t.tt",
                "security": "auto",
                "encryption": null
              }
            ]
          }
        ],
        "servers": null,
        "response": null
      },
      "streamSettings": {
        "network": "ws",
        "security": null,
        "tlsSettings": null,
        "tcpSettings": null,
        "kcpSettings": null,
        "wsSettings": {
          "connectionReuse": true,
          "path": "/v2ray",
          "headers": null
        },
        "httpSettings": null,
        "quicSettings": null
      },
      "mux": {
        "enabled": true,
        "concurrency": 8
      }
    },
    {
      "tag": "direct",
      "protocol": "freedom",
      "settings": {
        "vnext": null,
        "servers": null,
        "response": null
      },
      "streamSettings": null,
      "mux": null
    },
    {
      "tag": "block",
      "protocol": "blackhole",
      "settings": {
        "vnext": null,
        "servers": null,
        "response": {
          "type": "http"
        }
      },
      "streamSettings": null,
      "mux": null
    }
  ],
  "stats": null,
  "api": null,
  "dns": null,
  "routing": {
    "domainStrategy": "IPIfNonMatch",
    "rules": [
      {
        "type": "field",
        "port": null,
        "inboundTag": [
          "api"
        ],
        "outboundTag": "api",
        "ip": null,
        "domain": null
      }
    ]
  }
}`
)

func main() {
	url := "http://127.0.0.1:6543/add"
	//url := "http://127.0.0.1:6543/delete"
	contentType := "application/json;charset=utf-8"

	/*cmd := Cmd{ReqType: 12, FileName: "plugin"}*/
	//b, err := json.Marshal(cmd)
	//if err != nil {
	//log.Println("json format error:", err)
	//return
	//}

	/*body := bytes.NewBuffer(b)*/
	body := bytes.NewBuffer([]byte(jsCfg))

	resp, err := http.Post(url, contentType, body)
	if err != nil {
		log.Println("Post failed:", err)
		return
	}

	defer resp.Body.Close()

	content, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Println("Read failed:", err)
		return
	}

	log.Println("content:", string(content))

	fmt.Println("vim-go")
}
