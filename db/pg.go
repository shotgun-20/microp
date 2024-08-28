package db

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"sync"

	"github.com/jackc/pgx/v5"
)

/*
Храним параметры конфигурации для соединения с БД,
а так же ссылки на waitgroup и контекст для синхронизации.

Можно было все эти параметры сделать приватными и передавать значения
через Init, но не будем усложнять.
*/
type DbConnector struct {
	Username string
	Password string
	Host     string
	Port     string
	Database string
	Ctx      context.Context
	conn     *pgx.Conn
	WG       *sync.WaitGroup
}

/*
Устанавливаем соединение с БД по заданным параметрам.
Добавляем свой вотчдог, чтобы ещё и с вейтгруппой работать.
*/
func (db *DbConnector) Init() error {
	db.WG.Add(1)
	var err error
	dbUrl := fmt.Sprintf("postgres://%s:%s@%s:%s/%s", db.Username, db.Password, db.Host, db.Port, db.Database)
	db.conn, err = pgx.Connect(db.Ctx, dbUrl)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Unable to connect to database: %v\n", err)
		os.Exit(1)
	}
	go func() { // Это не обязательно
		// Но всё же введём явный контроль с помощью WG
		defer db.WG.Done()
		<-db.Ctx.Done()
		log.Print("shutting down database connection")
		err = db.conn.Close(db.Ctx)
		if err != nil {
			log.Print("error shutting down database connection:", err)
		}
	}()
	return err
}

// Дальнейшая работа с БД будет сведена к вызову хранимок
// Таким образом мы полностью исключаем возможность, что при возникновении
// какой-либо паузы между удостоверением пользователя и работой с контеном
// токен успеет устареть, а пользователя всё равно пропустят по устаревшему токену.
// Фактически, хранимки дают нам атомарные операции.

/*
Возвращаем токен или пустую строку, если креды не верны, а так же ошибку БД, если она была.
*/
func (db *DbConnector) AuthUser(user, pass, ip string) (string, error) {
	var err error
	var token string
	err = db.conn.QueryRow(db.Ctx, "SELECT get_token($1,$2,$3)", user, pass, ip).Scan(&token)
	if err != nil {
		return "", err
	}
	return token, err
}

/*
Проверяем валидность предъявленного токена.
Возвращаем валидность и ошибку БД, если была.

Токен всегда связан с IP, с которого было запрошено его создание.
Если токен приходит не с того IP, с которого был выдан, он не пройдёт валидацию.
*/
func (db *DbConnector) CheckAuth(token, ip string) (bool, error) {
	var err error
	var reply bool
	err = db.conn.QueryRow(db.Ctx, "SELECT check_token($1,$2)", token, ip).Scan(&reply)
	if err != nil {
		return reply, err
	}
	return reply, nil
}

/*
Загружаем бинарные данные в БД в пространство ассетов пользователя, которому принадлежит токен.

Валидность токена проверяется, эта проверка встроена в хранимку.
*/
func (db *DbConnector) UploadAsset(token, ip, asset string, content io.ReadCloser) (bool, error) {
	var err error
	var reply bool
	data, err := io.ReadAll(content)
	if err != nil {
		return false, err
	}
	err = db.conn.QueryRow(db.Ctx, "SELECT upload_asset($1,$2,$3,$4)", token, ip, asset, data).Scan(&reply)
	return reply, err
}

/*
Извлекаем из БД ассет, находящийся в пространстве владельца токена.

Валидность токена проверяется, эта проверка встроена в хранимку.
Сама хранимка возвращает RECORD, поэтому обёрнута в SELECT, разбирающий RECORD на поля.
*/
func (db *DbConnector) DownloadAsset(token, ip, asset string) (bool, bool, []byte, error) {
	var err error
	var auth, exists bool
	var data []byte
	err = db.conn.QueryRow(db.Ctx,
		"SELECT auth,exists,cont FROM download_asset($1,$2,$3) AS (auth BOOL, exists BOOL, cont BYTEA)",
		token, ip, asset).Scan(&auth, &exists, &data)
	return auth, exists, data, err
}

/*
Удаляем ассет, если токен прошёл валидацию и ассет существует.
Возвращаем результат валидации и существовал ли удаляемый ассет.
Если валидация не прошла, то признак существования всегда FALSE,
так как невозможно определить пространство имён.
*/
func (db *DbConnector) DelAsset(token, ip, asset string) (bool, bool, error) {
	var err error
	var auth, exists bool
	err = db.conn.QueryRow(db.Ctx,
		"SELECT auth,exists FROM delete_asset($1,$2,$3) AS (auth BOOL, exists BOOL)",
		token, ip, asset).Scan(&auth, &exists)
	return auth, exists, err
}

func (db *DbConnector) ListAsset(token, ip string) (bool, []string, error) {
	var err error
	var auth bool
	var data []string
	err = db.conn.QueryRow(db.Ctx,
		"SELECT auth,cont FROM list_asset($1,$2) AS (auth BOOL, cont TEXT[])",
		token, ip).Scan(&auth, &data)
	return auth, data, err
}
