// Command server runs the TTS workbench ("流变叠时台").
package main

import (
	"flag"
	"log"
	"net/http"

	"ttsbench/internal/store"
	"ttsbench/internal/tts"
	"ttsbench/internal/web"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:5520", "listen address")
	dbPath := flag.String("db", "tts.db", "SQLite database path")
	seed := flag.Int64("seed", 1183, "fixture seed used when the database is empty")
	flag.Parse()

	st, err := store.Open(*dbPath)
	if err != nil {
		log.Fatalf("打开数据库失败: %v", err)
	}
	defer st.Close()

	has, err := st.HasRun()
	if err != nil {
		log.Fatalf("读取数据库失败: %v", err)
	}
	if !has {
		if err := st.SaveRun(tts.NewRun(*seed)); err != nil {
			log.Fatalf("初始化 fixture 失败: %v", err)
		}
		log.Printf("数据库为空，已按种子 %d 生成演示数据", *seed)
	}

	srv := web.New(st)
	log.Printf("流变叠时台 listening on http://%s", *listen)
	log.Fatal(http.ListenAndServe(*listen, srv.Handler()))
}
