#!/bin/bash
# scripts/start_ibkr_gateway.sh
# ------------------------------
# Starts the official Interactive Brokers Client Portal Web API Gateway on https://localhost:5001

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

# Find Java (bundled with Trader Workstation or in system PATH)
JAVA_BIN="/Users/darianhickman/Applications/Trader Workstation/.install4j/jre.bundle/Contents/Home/bin/java"
if [ ! -f "$JAVA_BIN" ]; then
    JAVA_BIN=$(which java)
fi

if [ -z "$JAVA_BIN" ] || [ ! -f "$JAVA_BIN" ]; then
    echo "❌ Error: Java runtime not found. Please install Java (brew install openjdk) or Trader Workstation."
    exit 1
fi

echo "======================================================================================================================="
echo "🌐 STARTING INTERACTIVE BROKERS CLIENT PORTAL WEB API GATEWAY (Port 5001)"
echo "======================================================================================================================="
echo "☕ Using Java Runtime: $JAVA_BIN"
echo "📁 Gateway Working Dir: $ROOT_DIR/clientportal"
echo ""
echo "👉 Once launched, open https://localhost:5001 in your browser and log into Interactive Brokers."
echo "👉 Accept the local self-signed SSL certificate in your browser to complete authentication."
echo "======================================================================================================================="

cd "$ROOT_DIR/clientportal" || exit 1

export PATH="$(dirname "$JAVA_BIN"):$PATH"
export RUNTIME_PATH="root:dist/ibgroup.web.core.iblink.router.clientportal.gw.jar:build/lib/runtime/*"

exec "$JAVA_BIN" \
    -server \
    -Dvertx.disableDnsResolver=true \
    -Djava.net.preferIPv4Stack=true \
    -Dvertx.logger-delegate-factory-class-name=io.vertx.core.logging.SLF4JLogDelegateFactory \
    -cp "${RUNTIME_PATH}" \
    ibgroup.web.core.clientportal.gw.GatewayStart \
    --conf root/conf.yaml
