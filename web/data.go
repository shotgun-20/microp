package web

import "microp/db"

/*
Все возможные поля, которые может вернуть сервер в тех случаях,
когда применим ответ в JSON.
*/
type ResponseBody struct {
	Status string   `json:"status,omitempty"`
	Token  string   `json:"token,omitempty"`
	Error  string   `json:"error,omitempty"`
	Assets []string `json:"assets,omitempty"`
}

/*
Оборачиваем коннектор БД, чтобы хендлеры имели с ней связь.
*/
type HandlerPack struct {
	DB *db.DbConnector
}
