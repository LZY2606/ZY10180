// Command server runs the TTS workbench web service.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"ttsworkbench/internal/rheo"
	"ttsworkbench/internal/store"
	"ttsworkbench/internal/web"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:5520", "listen address")
	dbPath := flag.String("db", "tts.db", "SQLite database path")
	fixturePath := flag.String("fixtures", "fixtures/sweeps.json", "fixture JSON path")
	flag.Parse()

	raw, err := os.ReadFile(*fixturePath)
	if err != nil {
		log.Fatalf("读取 fixture 失败: %v", err)
	}
	var fx rheo.Fixture
	if err := json.Unmarshal(raw, &fx); err != nil {
		log.Fatalf("解析 fixture 失败: %v", err)
	}

	st, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	defer st.Close()

	empty, err := st.Empty()
	if err != nil {
		log.Fatalf("检查数据库失败: %v", err)
	}
	if empty {
		if err := st.ImportFixture(fx); err != nil {
			log.Fatalf("导入 fixture 失败: %v", err)
		}
		log.Printf("数据库为空，已导入 fixture（种子 %d，%d 条曲线）", fx.Seed, len(fx.Curves))
	}

	srv := web.NewServer(st, fx)
	fmt.Printf("流变叠时台 listening on http://%s\n", *listen)
	log.Fatal(http.ListenAndServe(*listen, srv))
}
