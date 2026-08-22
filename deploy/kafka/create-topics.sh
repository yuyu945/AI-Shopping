#!/usr/bin/env bash
set -euo pipefail

bootstrap_servers="${KAFKA_BOOTSTRAP_SERVERS:-kafka:9092}"
topics=(
  knowledge.document.ingest
  knowledge.chunk.embed
  behavior.events
  review.events
  inventory.reservation.confirm
  inventory.reservation.confirm.deadletter
)

for topic in "${topics[@]}"; do
  /opt/kafka/bin/kafka-topics.sh \
    --bootstrap-server "$bootstrap_servers" \
    --create \
    --if-not-exists \
    --topic "$topic" \
    --partitions 3 \
    --replication-factor 1
done
