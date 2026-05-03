# Docker Operations

## Commands
```bash
# === Full System ===
docker-compose up -d --build    # Start everything
docker-compose down             # Stop everything
docker-compose restart          # Restart everything
docker-compose ps               # Check status
docker-compose logs -f          # View logs real-time

# === Single Service ===
docker-compose up -d --build arl-worker    # Rebuild + restart
docker-compose up -d --build arl-dashboard # Rebuild dashboard UI
docker-compose logs -f arl-gateway         # View logs
docker-compose restart prometheus          # Restart

# === Info ===
docker stats                              # Resource usage
docker exec -it arl-gateway sh            # Shell in container
docker exec -it arl-dragonfly redis-cli   # Dragonfly CLI

# === Cleanup ===
docker-compose down -v         # Remove containers + volumes (reset data)
docker-compose down --rmi all  # Remove images
```

## Dragonfly Commands
```bash
docker exec -it arl-dragonfly redis-cli
> INFO     # Server info
> DBSIZE   # Number of keys
> LLEN ai_jobs  # Queue length
> KEYS *  # View all keys (careful on production)
> MEMORY USAGE <key>  # Memory of key
> FLUSHALL  # Delete all data (careful!)
```

---

*Back to [Manual](../MANUAL.md)*
