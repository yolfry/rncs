# rncs (RNC) DGii API - API RNC DGii

`rncs` es la API más rápida para consultar el RNC de la DGII, diseñada y desarrollada 100% en Go para ofrecer el máximo rendimiento. Gracias a su arquitectura optimizada, utiliza almacenamiento en memoria RAM para búsquedas ultra rápidas y permite recargar el archivo de la DGII en caliente sin reiniciar el servicio. El binario generado es altamente eficiente y ligero, ideal para despliegues en cualquier entorno.

## Características

- Búsqueda rápida y eficiente de RNC en memoria (RAM)
- Modo **CLI** para consultas puntuales
- Modo **API** HTTP con endpoints:
  - `GET /api/checkrnc/{RNC}`
  - `GET /api/checkcedula/{CEDULA}`
  - `POST /api/reload`
- Descarga y extracción automática del archivo CSV desde la DGII si no existe localmente
- Recarga en caliente del archivo CSV sin reiniciar el servicio
- Binario optimizado, 100% hecho en Go

## Requisitos

- Go >= 1.21
- Archivo `rncs.csv` en el directorio de ejecución (se crea/actualiza automáticamente)

## Instalación

```bash
git clone https://github.com/tu-usuario/rncs.git
cd rncs
go build -o src/rncs
# Opcionalmente, mueve el binario a tu PATH
sudo mv rncs /usr/local/bin/
```

## Uso

### Iniciar el servidor API

```bash
rncs --foreground [PUERTO]
```

- Si no especificas el puerto, usará `9922` por defecto.

### Consultar ayuda

```bash
rncs --help
```

## Actualización automática del archivo CSV

Para mantener siempre el archivo `rncs.csv` actualizado con la información más reciente de la DGII, solo necesitas crear una tarea cron que ejecute diariamente el endpoint `/api/reload` de la API. Esto permite recargar el archivo en caliente sin reiniciar el servicio.

Por ejemplo, puedes agregar la siguiente línea a tu crontab para que se ejecute cada 24 horas:

```sh
0 3 * * * curl -X POST http://localhost:9922/api/reload
```

## Despliegue con Docker Compose

Puedes desplegar fácilmente la API y la tarea de recarga automática usando Docker Compose.  
Guarda el siguiente contenido en un archivo llamado `docker-compose.yaml`:

```yaml
version: "3.9"

services:
  rncs-api:
    image: yolfryr/rncs:latest
    container_name: rncs-api
    ports:
      - "9922:9922"
    command: ["--foreground", "9922"]
    restart: unless-stopped

  cron-reload:
    image: curlimages/curl:latest
    container_name: rncs-cron-reload
    depends_on:
      - rncs-api
    entrypoint: ["/bin/sh", "-c"]
    command: |
      while true; do
        curl -X POST http://rncs-api:9922/api/reload;
        sleep 86400;
      done
```

```bash
## Luego ejecuta:
docker compose up -d
```



## Autor

Desarrollado por **Yolfry R.**  
Correo: yolfri1997@gmail.com

## Licencia

MIT
