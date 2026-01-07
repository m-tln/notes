#!/bin/bash
# loadtest-https.sh

echo "=== Load Testing HTTPS Service Mesh ==="

# Тест через nginx (HTTPS порт 443)
echo "1. Creating 10 notes via HTTPS (nginx:443)..."
for i in {1..10}; do
  curl -k -X POST https://localhost/notes \
    -H "Content-Type: application/json" \
    -d "{\"title\":\"HTTPS Note $i\",\"content\":\"Content $i via nginx\"}"
  echo
  sleep 0.1
done

echo "2. Checking distribution across instances..."
echo "=== App1 logs ==="
docker-compose logs app1 --tail=20 | grep -i "created\|successfully" || echo "No matches in app1"

echo -e "\n=== App2 logs ==="
docker-compose logs app2 --tail=20 | grep -i "created\|successfully" || echo "No matches in app2"

echo -e "\n=== App3 logs ==="
docker-compose logs app3 --tail=20 | grep -i "created\|successfully" || echo "No matches in app3"

echo -e "\n3. Getting all notes..."
curl -k https://localhost/notes
echo

echo "4. Load balancer status..."
curl -k https://localhost:443/status
echo