package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"

	"microp/db"
	"microp/web"
)

func NewServer(hp *web.HandlerPack) http.Handler {
	mux := http.NewServeMux()
	addRoutes(mux, hp)
	var handler http.Handler = mux
	return handler
}

/*
Маршрутизация запросов
*/
func addRoutes(mux *http.ServeMux, hp *web.HandlerPack) {
	/*
		Поскольку аутентификация вынесена в ту же БД и обрабатывается
		теми же хранимками - не усложняем, всё делаем в один слой,
		без декораторов и т.д.
		По сути, на любой запрос нам нужно только дёрнуть хранимку.
	*/
	mux.HandleFunc("POST /api/upload-asset/{asset}", hp.PostAsset)
	mux.HandleFunc("GET /api/asset/{asset}", hp.GetAsset)
	mux.HandleFunc("DELETE /api/asset/{asset}", hp.DelAsset)
	mux.HandleFunc("GET /api/asset", hp.ListAsset)
	mux.HandleFunc("POST /api/auth", hp.AuthUser)
	mux.HandleFunc("GET /health", hp.HealthCheck)
	mux.Handle("/", http.NotFoundHandler())
}

/*
Сюда обёрнут запуск всей пачки и контроль за её корректным выключением
по сигналу ОС.

Требований по конфигурирванию сервера в спеке нет, поэтому адрес и порт
для прослушивания просто хардкодим.
*/
func run(ctx context.Context) error {
	var wg sync.WaitGroup
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt)
	defer cancel()
	// Коннектор к БД
	myDb := db.DbConnector{Username: os.Getenv("DB_USER"),
		Password: os.Getenv("DB_PASS"),
		Host:     os.Getenv("DB_HOST"),
		Port:     os.Getenv("DB_PORT"),
		Database: os.Getenv("DB_DATABASE"),
		Ctx:      ctx, WG: &wg}
	if err := myDb.Init(); err != nil {
		log.Panicf("cannot connect to database: %s\n", err)
	}

	// Хэндлеры с коннектором к БД
	handlers := web.HandlerPack{DB: &myDb}

	srv := NewServer(&handlers)
	httpServer := &http.Server{
		Addr:    net.JoinHostPort("0.0.0.0", "8086"),
		Handler: srv,
	}
	go func() { // Запускаем HTTP сервер в фоне
		log.Printf("listening on %s\n", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("error listening and serving: %s\n", err)
		}
	}()
	wg.Add(1)
	go func() { // Вотчдог для корректного останова
		defer wg.Done()
		<-ctx.Done()
		log.Print("shutting down web server")
		if err := httpServer.Shutdown(ctx); err != nil {
			log.Printf("error shutting down http server: %s\n", err)
		}
	}()
	wg.Wait()
	return nil
}

/*
Эту функцию хочется иметь максимально маленькой и простой. Кнопка "пуск".
*/
func main() {
	log.Print("=== Let's get the party started! ===")
	ctx := context.Background()
	if err := run(ctx); err != nil {
		log.Printf("%s\n", err)
		os.Exit(1)
	}
}
