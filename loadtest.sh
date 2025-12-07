for i in {1..10}; do
  curl -X POST http://localhost:8080/notes \
    -H "Content-Type: application/json" \
    -d "{\"title\":\"Note $i\",\"content\":\"Content $i\"}"
  echo
done

echo "1\n"
docker-compose logs app1 | grep "created note"
echo "\n\n2\n"
docker-compose logs app2 | grep "created note"
echo "\n\n3\n"
docker-compose logs app3 | grep "created note"
