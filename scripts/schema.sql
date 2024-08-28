create extension if not exists pgcrypto;

-- таблица с пользователями
create table if not exists users (
    id bigserial primary key,
    login text not null unique,
    password_hash text not null,
    created_at timestamptz not null default now()
);

-- таблица сессий
create table if not exists sessions (
    id text primary key default encode(gen_random_bytes(16),'hex'),
    uid bigint not null,
    -- user id
    ip_addr text not null,
    created_at timestamptz not null default now()
);

-- таблица с файлами
create table if not exists assets (
    name text not null,
    uid bigint not null,
    data bytea not null,
    -- user id
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now(), -- полезно знать, обновлялся ли ассет
    primary key (name, uid)
);

-- тестовые пользователи
-- нам надо больше одного, чтобы всё проверить корректно
insert into users
    (login, password_hash)
    values
    ('alice', encode(digest('secret', 'md5'),'hex'))
on conflict do nothing;
insert into users
    (login, password_hash)
    values
    ('bob', encode(digest('mystery', 'md5'),'hex'))
on conflict do nothing;

-- -- хранимки -- --

-- получение нового токена
CREATE OR REPLACE FUNCTION get_token(name TEXT,pass TEXT, ipaddr TEXT)
    RETURNS TEXT AS $function$
    DECLARE	token TEXT;
BEGIN
    INSERT INTO sessions (uid,ip_addr)
        SELECT id,ipaddr AS ip_addr FROM users WHERE login=name AND password_hash=encode(digest(pass, 'md5'),'hex') LIMIT 1
        on conflict do nothing;
    SELECT id FROM sessions
        WHERE uid=(SELECT id FROM users WHERE login=name AND password_hash=encode(digest(pass, 'md5'),'hex'))
        AND created_at > (NOW() - INTERVAL '1 DAY')
        ORDER BY created_at DESC LIMIT 1 INTO token;
    SELECT coalesce(token,'') INTO token; -- Затычка, если не нашлось. Иначе парсер выдаст ворнинг
    RETURN token;
END;
$function$ LANGUAGE plpgsql;

-- получаем UID по токену
CREATE OR REPLACE FUNCTION get_uid(token TEXT, ipaddr TEXT)
    RETURNS BIGINT AS $function$
    DECLARE user_id BIGINT;
BEGIN
    SELECT INTO user_id 
        (SELECT uid AS user_id FROM sessions
            WHERE id=token 
            AND ip_addr=ipaddr
            AND created_at > (NOW() - INTERVAL '1 DAY')
            AND created_at=(
                SELECT max(created_at) FROM sessions
                    WHERE uid=(SELECT uid FROM sessions WHERE id=token)));
    RETURN user_id;
END;
$function$ LANGUAGE plpgsql;

-- проверка токена
CREATE OR REPLACE FUNCTION check_token(token TEXT, ipaddr TEXT)
    RETURNS BOOL AS $function$
    DECLARE actual BOOL;
BEGIN
    SELECT INTO actual ( SELECT user_id IS NOT NULL
        FROM (SELECT get_uid(token,ipaddr) as user_id));
    RETURN actual;
END;
$function$ LANGUAGE plpgsql;

-- заливка контента
CREATE OR REPLACE FUNCTION upload_asset(token TEXT, ipaddr TEXT, asset TEXT, cont BYTEA)
    RETURNS BOOL AS $function$
    DECLARE
        user_id BIGINT;
        actual BOOL;
BEGIN
    SELECT INTO user_id (SELECT get_uid(token,ipaddr) as user_id);

    UPDATE assets SET data=cont, updated_at=now()
        WHERE name=asset
        AND uid=user_id;

    INSERT INTO assets (uid,name,data) VALUES (user_id,asset,cont)
    on conflict do nothing;
    
    SELECT EXISTS INTO actual
        (SELECT name,uid FROM assets
            WHERE uid=user_id
            AND name=asset);
    RETURN actual;
END;
$function$ LANGUAGE plpgsql;

-- выгрузка контента
CREATE OR REPLACE FUNCTION download_asset(token TEXT, ipaddr TEXT, asset TEXT)
    RETURNS RECORD AS $function$
    DECLARE
        reply RECORD;
        user_id BIGINT;
BEGIN
    SELECT INTO user_id (SELECT get_uid(token,ipaddr) as user_id);
    IF user_id IS NOT NULL AND (SELECT EXISTS (SELECT name,uid FROM ASSETS WHERE name=asset AND UID=user_id)) THEN
        SELECT TRUE, TRUE, (SELECT data FROM assets
            WHERE uid=user_id
            AND name=asset) INTO reply;
    ELSE
        SELECT CASE WHEN user_id IS NULL THEN FALSE ELSE TRUE END, FALSE,'\xDEADBEEF'::bytea INTO reply;
        -- тело ассета возвращается всегда не пустое, чтобы парсер не сыпал ворнингами в лог
    END IF;
    RETURN reply;
END;
$function$ LANGUAGE plpgsql;

-- список ассетов
CREATE OR REPLACE FUNCTION list_asset(token TEXT, ipaddr TEXT)
    RETURNS RECORD AS $function$
    DECLARE
        reply RECORD;
        user_id BIGINT;
BEGIN
    SELECT INTO user_id (SELECT get_uid(token,ipaddr) as user_id);
    IF user_id IS NOT NULL THEN
        SELECT TRUE AS allowed,(
            SELECT array(SELECT assets.name FROM assets WHERE uid=user_id
                ORDER BY updated_at DESC)
            ) AS names INTO reply;
    ELSE
        SELECT CASE WHEN user_id IS NULL THEN FALSE ELSE TRUE END AS allowed, '{}'::TEXT[] INTO reply;
    END IF;
    RETURN reply;
END;
$function$ LANGUAGE plpgsql;

-- удаление ассета
CREATE OR REPLACE FUNCTION delete_asset(token TEXT, ipaddr TEXT, asset TEXT)
    RETURNS RECORD AS $function$
    DECLARE
        reply RECORD;
        user_id BIGINT;
        exst BOOL;
BEGIN
    SELECT INTO user_id (SELECT get_uid(token,ipaddr) as user_id);
    SELECT INTO exst (SELECT EXISTS (SELECT name,uid FROM ASSETS WHERE name=asset AND UID=user_id));
    -- можно было куда меньше запросов сделать, но нам надо вернуть
    -- и допуск, и существование
    IF user_id IS NOT NULL THEN
        DELETE FROM assets WHERE uid=user_id AND name=asset;
        SELECT TRUE, exst INTO reply;
    ELSE
        SELECT FALSE, FALSE INTO reply;
    END IF;
    RETURN reply;
END;
$function$ LANGUAGE plpgsql;