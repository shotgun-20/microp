package web

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
)

/*
Вспомогательная функция, извлекает IP клиента из запроса
*/
func (hp *HandlerPack) getIP(r *http.Request) string {
	addrInfo := strings.Split(r.RemoteAddr, ":")
	return strings.Join(addrInfo[:len(addrInfo)-1], ":")
}

/*
Вспомогательная функция, извлекает токен из запроса
*/
func (hp *HandlerPack) getToken(r *http.Request) string {
	authorization := r.Header.Get("Authorization") // нечувствительно к регистру
	return strings.TrimSpace(strings.Replace(authorization, "Bearer", "", 1))
}

/*
Примитивный хелсчек-плейсхолдер, просто показывает, что сервер ещё не совсем завис.
*/
func (hp *HandlerPack) HealthCheck(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("got / request\n")
	reply, _ := json.Marshal(ResponseBody{Status: "ok"})
	w.Write(reply)
}

/*
Принимаем от клиента креды в JSON. Если удалось их распарсить,
то передаём в храниму БД, которая проверит креды и вернёт токен,
если валидация прошла успешно.
*/
func (hp *HandlerPack) AuthUser(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var reply []byte
	reqBody := make(map[string]interface{})
	w.Header().Set("Content-Type", "application/json")
	err := json.NewDecoder(r.Body).Decode(&reqBody)
	if err != nil {
		// Ошбку парсера логировать не будем, от клиента может приходить что угодно
		// Если всё логировать - напрасно зафлудим лог.
		w.WriteHeader(http.StatusBadRequest)
		reply, _ = json.Marshal(ResponseBody{Error: err.Error()})
		w.Write(reply)
		return
	}
	token, err := hp.DB.AuthUser(reqBody["login"].(string), reqBody["password"].(string), hp.getIP(r))
	if err != nil || token == "" || token == "DENIED" {
		w.WriteHeader(http.StatusUnauthorized)
		reply, _ = json.Marshal(ResponseBody{Error: "invalid login/password"})
		if err != nil {
			log.Print("AuthUser: ", err) // Логируем ошибку обращения к БД
		}
	} else {
		reply, _ = json.Marshal(ResponseBody{Token: token})
	}
	w.Write(reply)
}

/*
Загрузка ассета. Сначала проверяем только валидность токена,
затем выполняем собственно загрузку. Получается два запроса на одно действие,
но это позволяет избежать абуза БД лишним тарфиком.
*/
func (hp *HandlerPack) PostAsset(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var reply []byte
	asset := r.PathValue("asset")
	token := hp.getToken(r)
	ipAddr := hp.getIP(r)
	w.Header().Set("Content-Type", "application/json")
	/*
		Сначала проверить токен, чтобы не давать абузить базу тоннами данных без авторизации.
		Если проверять токен только в хранимке загрузки, это будет означать, что тело запроса
		будет передано в БД в любом случае, даже без авторизации. Этого лишнего трафика лучше
		не допустить.
	*/
	if ok, err := hp.DB.CheckAuth(token, ipAddr); err != nil || !ok {
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			reply, _ = json.Marshal(ResponseBody{Error: err.Error()})
			log.Print("PostAsset: ", err) // Логируем ошибку обращения к БД
		} else {
			w.WriteHeader(http.StatusUnauthorized)
			reply, _ = json.Marshal(ResponseBody{Error: "unauthorized"})
		}
		w.Write(reply)
		return
	}
	// Токен проверили, теперь можно собственно заливку делать
	ok, err := hp.DB.UploadAsset(token, ipAddr, asset, r.Body)
	if err != nil || !ok {
		if err != nil { // Ошибка выполнения запроса к БД
			w.WriteHeader(http.StatusInternalServerError)
			reply, _ = json.Marshal(ResponseBody{Error: err.Error()})
			log.Print("UploadAsset: ", err) // Логируем ошибку обращения к БД
		} else if !ok { // БД сообщила, что почему-то ассет не создался
			w.WriteHeader(http.StatusBadRequest)
			reply, _ = json.Marshal(ResponseBody{Status: "cannot create asset"})
		}
	} else {
		reply, _ = json.Marshal(ResponseBody{Status: "ok"})
	}
	w.Write(reply)
}

/*
Проверяем токен клиента и наличие ассета. Если обе проверки успешны,
то возвращаем контент. Если что-то не прошло, отдаём соответствующую
ошибку.
*/
func (hp *HandlerPack) GetAsset(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var reply []byte
	asset := r.PathValue("asset")
	token := hp.getToken(r)
	ipAddr := hp.getIP(r)
	/*
		В хранимку встроена проверка токена. Если проверка пройдёт, пользователь получит контент.
		Если проверка токена не пройдёт, клиент получит 403.
		Если ассет не будет найден, вернём клиенту 404.
		Удостоверенный клиент всегда видит только своё пространство ассетов,
		а неудостоверенный не видит ничего, поэтому обратиться к
		чужому ассету невозможно в принципе.
	*/
	auth, exists, data, err := hp.DB.DownloadAsset(token, ipAddr, asset)
	if err != nil || !auth || !exists {
		w.Header().Set("Content-Type", "application/json")
		if err != nil { // Ошибка в работе с БД
			w.WriteHeader(http.StatusInternalServerError)
			reply, _ = json.Marshal(ResponseBody{Error: err.Error()})
			log.Print("GetAsset: ", err) // Логируем ошибку обращения к БД
		} else if !auth { // Токен не прошёл валидацию
			w.WriteHeader(http.StatusUnauthorized)
			reply, _ = json.Marshal(ResponseBody{Error: "unauthorized"})
		} else if !exists { // запрошенного ассета нет в пространстве пользователя
			w.WriteHeader(http.StatusNotFound)
			reply, _ = json.Marshal(ResponseBody{Error: "not found"})
		}
		w.Write(reply)
		return
	}
	// Если добрались сюда - то ошибок нет, отдаём контент
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Write(data)
}

/*
Удаление ассета, если он был. Если не было, возвращаем 404.
Функционал пакетного удаления не затребован, поэтому удаляем ассеты только по одному.
*/
func (hp *HandlerPack) DelAsset(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var reply []byte
	asset := r.PathValue("asset")
	token := hp.getToken(r)
	ipAddr := hp.getIP(r)
	/*
		В хранимку встроена проверка токена. Если проверка пройдёт, будет попытка удалить
		ассет. Возвращается два значения: допуск токена и существование ассета.
		Если допуск FALSE, то существование тоже всегда FALSE.
	*/
	auth, exists, err := hp.DB.DelAsset(token, ipAddr, asset)
	if err != nil || !auth || !exists {
		w.Header().Set("Content-Type", "application/json")
		if err != nil { // Ошибка в работе с БД, всегда приоритет
			w.WriteHeader(http.StatusInternalServerError)
			reply, _ = json.Marshal(ResponseBody{Error: err.Error()})
			log.Print("GetAsset: ", err) // Логируем ошибку обращения к БД
		} else if !auth { // Токен не прошёл валидацию
			w.WriteHeader(http.StatusUnauthorized)
			reply, _ = json.Marshal(ResponseBody{Error: "unauthorized"})
		} else if !exists { // запрошенного ассета нет в пространстве пользователя
			w.WriteHeader(http.StatusNotFound)
			reply, _ = json.Marshal(ResponseBody{Error: "not found"})
		}
		w.Write(reply)
		return
	}
	// Если добрались сюда - то ошибок нет, отдаём контент
	reply, _ = json.Marshal(ResponseBody{Status: "ok"})
	w.Write(reply)
}

/*
Список ассетов для прошедшего удостоверение пользователя.
Список может быть пустым.
*/
func (hp *HandlerPack) ListAsset(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var reply []byte
	token := hp.getToken(r)
	ipAddr := hp.getIP(r)
	/*
		В хранимку встроена проверка токена. Если проверка пройдёт, пользователь получит список.
		Список может быть и пустым, если удостоверенный пользователь не имеет ассетов.
		Если проверка токена не пройдёт, клиент получит 403.
	*/
	auth, data, err := hp.DB.ListAsset(token, ipAddr)
	if err != nil || !auth {
		w.Header().Set("Content-Type", "application/json")
		if err != nil { // Ошибка в работе с БД
			w.WriteHeader(http.StatusInternalServerError)
			reply, _ = json.Marshal(ResponseBody{Error: err.Error()})
			log.Print("ListAsset: ", err) // Логируем ошибку обращения к БД
		} else if !auth { // Токен не прошёл валидацию
			w.WriteHeader(http.StatusUnauthorized)
			reply, _ = json.Marshal(ResponseBody{Error: "unauthorized"})
		}
		w.Write(reply)
		return
	}
	// Если добрались сюда - то ошибок нет, отдаём контент
	reply, _ = json.Marshal(ResponseBody{Assets: data})
	w.Write(reply)
}
