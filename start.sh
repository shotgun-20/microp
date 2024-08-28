#!/bin/bash

export PASS=0
export FAIL=0

assert () {
    if [ "$1" == "$2" ]
    then
        echo -e "OK"
        PASS=$((PASS + 1))
    else
        echo -e "FAIL"
        FAIL=$((FAIL + 1))
    fi
}

MICROP_HOST=localhost
MICROP_PORT=8080
MICROP_ADRESS="${MICROP_HOST}:${MICROP_PORT}"

echo "= Init modules"
go mod init microp
go mod tidy
echo "= Building binary"
go build
stat ./microp
echo "= Building container image"
docker build --network=host -t microp .
echo ""
echo "== Done with preparations"
echo "= Setting up testing suite"
rm -f test/*.json
rm -f test/*.tmp
docker compose up -d
echo "= Waiting 5 seconds to let things settle"
sleep 5
echo "== Ready to perform tests"
echo ""

echo -n "= Trying to get token with wrong credentials 'alice:nopass': "
OUT=`curl -si -d '{"login":"alice","password":"nopass"}' http://${MICROP_ADRESS}/api/auth|tail -n1`
assert "${OUT}" '{"error":"invalid login/password"}'

echo -n "= Getting token for user 'alice:secret': "
curl -s -d '{"login":"alice","password":"secret"}' http://${MICROP_ADRESS}/api/auth -o test/alice.json
ALICE_TOKEN=`jq -r '.token' test/alice.json`
OUT=${#ALICE_TOKEN}
assert "${OUT}" "32"

echo -n "= Uploading alice.txt as 'book' as user 'alice': "
OUT=`curl -s -X POST -H "Authorization: Bearer ${ALICE_TOKEN}" http://${MICROP_ADRESS}/api/upload-asset/book -d @test/alice.txt|tail -n1`
assert "${OUT}" '{"status":"ok"}'

echo -n "= Getting token for user 'bob:mystery': "
curl -s -d '{"login":"bob","password":"mystery"}' http://${MICROP_ADRESS}/api/auth -o test/bob.json
BOB_TOKEN=`jq -r '.token' test/bob.json`
OUT=${#BOB_TOKEN}
assert "${OUT}" "32"

echo -n "= Uploading bob.txt as 'song' as user 'bob': "
OUT=`curl -s -X POST -H "Authorization: Bearer ${BOB_TOKEN}" http://${MICROP_ADRESS}/api/upload-asset/song -d @test/bob.txt|tail -n1`
assert "${OUT}" '{"status":"ok"}'

echo -n "= Trying to fetch alice's asset 'book' using token: "
curl -s -H "Authorization: Bearer ${ALICE_TOKEN}" http://${MICROP_ADRESS}/api/asset/book -o test/alice.txt.tmp
assert "" `diff test/alice.txt test/alice.txt.tmp`

echo -n "= User 'alice' is trying to get bob's asset 'song' using own token: "
OUT=`curl -si -H "Authorization: Bearer ${ALICE_TOKEN}" http://${MICROP_ADRESS}/api/asset/song|tail -n1`
assert "${OUT}" '{"error":"not found"}'

echo -n "= Trying to get an asset ('book' in alice's namespace) without any token at all: "
OUT=`curl -si http://${MICROP_ADRESS}/api/asset/book|tail -n1`
assert "${OUT}" '{"error":"unauthorized"}'

echo -n "= Trying to get asset list without any token at all: "
OUT=`curl -si http://${MICROP_ADRESS}/api/asset|tail -n1`
assert "${OUT}" '{"error":"unauthorized"}'

echo -n "= User 'alice' requsts new token, thus invalidating the old one: "
curl -s -d '{"login":"alice","password":"secret"}' http://${MICROP_ADRESS}/api/auth -o test/alice.json
ALICE_TOKEN_NEW=`jq -r '.token' test/alice.json`
OUT=${#ALICE_TOKEN_NEW}
assert "${OUT}" "32"

echo -n "= Trying to fetch alice's asset 'book' using OLD token: "
OUT=`curl -s -H "Authorization: Bearer ${ALICE_TOKEN}" http://${MICROP_ADRESS}/api/asset/book|tail -n1`
assert "${OUT}" '{"error":"unauthorized"}'

echo -n "= User 'alice' updates own asset using NEW token: "
OUT=`curl -s -X POST -H "Authorization: Bearer ${ALICE_TOKEN_NEW}" http://${MICROP_ADRESS}/api/upload-asset/book -d @test/update.txt|tail -n1`
assert "${OUT}" '{"status":"ok"}'

echo -n "= Downloading updated 'book' for user 'alice' and check if it is updated: "
curl -s -H "Authorization: Bearer ${ALICE_TOKEN_NEW}" http://${MICROP_ADRESS}/api/asset/book -o test/alice.txt.tmp
assert "" `diff test/update.txt test/alice.txt.tmp`

echo -n "= User 'alice' uploads one more asset 'bonus': "
OUT=`curl -s -X POST -H "Authorization: Bearer ${ALICE_TOKEN_NEW}" http://${MICROP_ADRESS}/api/upload-asset/bonus -d @test/bonus.txt|tail -n1`
assert "${OUT}" '{"status":"ok"}'

echo -n "= User 'alice' uploads asset with wrong token: "
OUT=`curl -s -X POST -H "Authorization: Bearer somewrongtoken" http://${MICROP_ADRESS}/api/upload-asset/bonus -d @test/bonus.txt|tail -n1`
assert "${OUT}" '{"error":"unauthorized"}'

echo -n "= User 'alice' uploads asset with no token at all: "
OUT=`curl -s -X POST http://${MICROP_ADRESS}/api/upload-asset/bonus -d @test/bonus.txt|tail -n1`
assert "${OUT}" '{"error":"unauthorized"}'

echo -n "= Trying to delete an asset with other user's token: "
OUT=`curl -s -X DELETE -H "Authorization: Bearer ${BOB_TOKEN}" http://${MICROP_ADRESS}/api/asset/book|tail -n1`
assert "${OUT}" '{"error":"not found"}'

echo -n "= Trying to delete an asset with no token at all: "
OUT=`curl -s -X DELETE http://${MICROP_ADRESS}/api/asset/book|tail -n1`
assert "${OUT}" '{"error":"unauthorized"}'

echo -n "= User 'alice' request asset list: "
OUT=`curl -s -H "Authorization: Bearer ${ALICE_TOKEN_NEW}" http://${MICROP_ADRESS}/api/asset|tail -n1`
assert "${OUT}" '{"assets":["bonus","book"]}'

echo -n "= User 'alice' deletes asset 'book': "
OUT=`curl -s -X DELETE -H "Authorization: Bearer ${ALICE_TOKEN_NEW}" http://${MICROP_ADRESS}/api/asset/book|tail -n1`
assert "${OUT}" '{"status":"ok"}'

echo -n "= User 'alice' request asset list again: "
OUT=`curl -s -H "Authorization: Bearer ${ALICE_TOKEN_NEW}" http://${MICROP_ADRESS}/api/asset|tail -n1`
assert "${OUT}" '{"assets":["bonus"]}'

echo -n "= User 'alice' deletes the last remaining asset: "
OUT=`curl -s -X DELETE -H "Authorization: Bearer ${ALICE_TOKEN_NEW}" http://${MICROP_ADRESS}/api/asset/bonus|tail -n1`
assert "${OUT}" '{"status":"ok"}'

echo -n "= User 'alice' request asset list once more (there are none already): "
OUT=`curl -s -H "Authorization: Bearer ${ALICE_TOKEN_NEW}" http://${MICROP_ADRESS}/api/asset|tail -n1`
assert "${OUT}" '{}'

echo -n "= User 'bob' tries request with redirect to HTTPS: "
MICROP_ADRESS="${MICROP_HOST}:80"
OUT=`curl -s -k --location-trusted -L -H "Authorization: Bearer ${BOB_TOKEN}" http://${MICROP_ADRESS}/api/asset|tail -n1`
assert "${OUT}" '{"assets":["song"]}'

echo -e "\n=== Summary ==="
echo "Tests PASSED: ${PASS}"
echo "Tests FAILED: ${FAIL}"
echo ""
echo "== Cleaning up"

docker compose down --volumes
exit ${FAIL}
