#!/bin/sh
set -e

mkdir -p config

if [ -f config/idp-private.pem ] && [ -f config/idp-public.pem ]; then
  echo "Chaves IdP já existem em config/. Use 'make gen-idp-keys-force' para regenerar."
  exit 0
fi

openssl ecparam -genkey -name prime256v1 -noout -out config/idp-private.pem
openssl ec -in config/idp-private.pem -pubout -out config/idp-public.pem

echo "Chaves IdP geradas:"
echo "  Privada : config/idp-private.pem  (não versionar)"
echo "  Pública : config/idp-public.pem   (não versionar)"
