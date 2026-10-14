# Completist

**El gestor de ROMs que te dice lo que te falta.**

Completist compara tus ROMs con los DAT de No-Intro y te enseña, sistema por sistema, lo que
tienes, lo que te falta y qué archivos están dañados, duplicados o sin identificar. Funciona en tu
NAS, tu servidor Unraid o tu ordenador, como un solo programa pequeño con su base de datos dentro.

[English](README.md)

![Inicio: cuánto tienes de la colección, una casilla por juego](docs/screenshots/home.png)

> Completist es joven: ya tiene los sistemas de cartucho, los DAT, la colección y sus recuentos.
> Después llegan los sistemas de disco, la compresión y el renombrado (ver el [plan](#plan)).

## Qué hace

- **Te dice lo que te falta.** Cada sistema muestra cuántos juegos tienes, cuántos hay y cuáles
  faltan, con los que faltan en su sitio de la cuadrícula, en gris.
- **Cuenta a tu manera.** Una versión de cada juego (vale cualquier región o revisión), cada
  lanzamiento oficial, o el DAT entero con prototipos y homebrew. Un modo para todos los sistemas
  y otro para cada uno si quieres.
- **Reconoce los juegos por su contenido, no por su nombre.** Los archivos se identifican por sus
  sumas de comprobación, así que un archivo mal nombrado también se encuentra. Tiene en cuenta las
  cabeceras de copiadora (NES, SNES, Lynx, Atari 7800, PC Engine) y los órdenes de bytes de
  Nintendo 64. También lee archivos zip.
- **Te enseña lo que hay que revisar**: archivos con el nombre de un juego pero otro contenido
  (malos volcados, hacks, archivos dañados), archivos que no están en el DAT y duplicados.
- **Mantiene los DAT al día**: los descarga de libretro-database y los actualiza cada semana, o
  importa tus propios DAT (Logiqx XML o clrmamepro).
- **Se ve bien**: portadas, capturas, una página por juego con sus versiones y regiones, una
  interfaz oscura que funciona en el móvil, en español e inglés. Cada imagen se descarga una vez y
  se guarda, así que las páginas cargan al momento, también sin internet.
- **Se actualiza solo**: detecta cuando se añaden, quitan o cambian archivos y vuelve a escanear;
  las carpetas de red se revisan cada dos minutos. Los archivos que no cambian no se leen dos
  veces, así que 10.000 archivos se comprueban en menos de un segundo.
- **Te avisa** por Discord, Telegram o cualquier webhook: juegos nuevos en tu colección, sets que
  completas, archivos dañados y entradas nuevas en los DAT.
- **Guarda una copia** de las bibliotecas, los DAT, los ajustes y los usuarios cada semana, y
  conserva las cinco últimas; también se puede descargar y restaurar desde los ajustes.
- **Pensado para un servidor**: cuentas de usuario (administrador o solo lectura), protección
  contra quien intenta adivinar contraseñas, tareas en vivo, API JSON, bibliotecas de solo lectura e
  imagen Docker para amd64 y arm64.

| | |
| --- | --- |
| ![Un sistema: portadas y los juegos que faltan en gris](docs/screenshots/system.png) | ![Un juego: sus versiones y tus archivos](docs/screenshots/game.png) |

Las capturas se hicieron sin acceso al servidor de portadas, por eso se ven las portadas que
Completist dibuja cuando un juego no tiene.

## Sistemas

Sistemas de cartucho con DAT de No-Intro: NES, Famicom Disk System, Super Nintendo, Nintendo 64,
Virtual Boy, Game Boy, Game Boy Color, Game Boy Advance, Nintendo DS, Pokémon Mini, SG-1000,
Master System, Mega Drive, 32X, Game Gear, PC Engine, Atari 2600, Atari 7800, Lynx, Neo Geo Pocket
(y Color), WonderSwan (y Color), ColecoVision, Intellivision y Vectrex.

## Empezar

### Docker

```sh
docker run -d --name completist --restart unless-stopped \
  -p 3000:3000 \
  -e PUID=1000 -e PGID=1000 -e TZ=Europe/Madrid \
  -v /ruta/a/appdata:/data \
  -v /ruta/a/tus/roms:/roms \
  delfus77/completist
```

Abre http://localhost:3000. La carpeta montada en `/roms` se añade como biblioteca en el primer
arranque. También puedes usar [docker-compose.yml](docker-compose.yml).

| Ajuste | Para qué sirve |
| --- | --- |
| `/data` | La base de datos y los ajustes |
| `/roms` | Tus ROMs. Añade `:ro` para montarla de solo lectura: Completist solo las lee |
| `PUID`, `PGID` | El usuario y el grupo dueños de tus archivos (`id` en el equipo). La app nunca se ejecuta como root |
| `TZ` | Tu zona horaria |

### Unraid

Hasta que esté en Community Applications, instala la plantilla a mano. En una terminal del servidor:

```sh
wget -O /boot/config/plugins/dockerMan/templates-user/my-completist.xml \
  https://raw.githubusercontent.com/DeLFuS77/completist/main/templates/completist.xml
```

Después ve a **Docker > Add Container** y elige `completist`. En **ROMs** pon tu carpeta compartida.

### Windows, macOS y Linux

Compílalo (ver [Compilar](#compilar)) y ejecuta `completist`. Guarda sus datos en una carpeta
`data` junto al programa, o donde diga `COMPLETIST_DATA_DIR`. Abre http://localhost:3000.

## Primer arranque

1. Añade la carpeta con tus ROMs en **Ajustes > Bibliotecas**. Las subcarpetas con el nombre de un
   sistema (`snes`, `Mega Drive`, `gba`...) le dicen a Completist de qué es cada archivo; si no, lo
   decide la extensión. En cuanto está su DAT, un archivo se encuentra por su contenido igualmente.
2. Descarga los DAT de tus sistemas: con un clic en el inicio, o eligiéndolos en **Ajustes > DAT**.
3. Aparece tu colección, sistema por sistema.

## Cómo se cuentan los juegos

| Modo | Cuenta |
| --- | --- |
| Una versión de cada juego | Cada juego una vez, sea cual sea su región o revisión. Se dejan fuera betas, demos, prototipos, juegos sin licencia, hacks y BIOS |
| Cada lanzamiento oficial | Cada región y revisión de los juegos oficiales por separado |
| Cada entrada del DAT | El set entero |

Con una versión de cada juego, las versiones se agrupan por su título en el DAT, y las regiones
preferidas deciden cuál se muestra cuando no tienes ninguna.

## Usuarios

Sin usuarios, cualquiera en tu red local puede usar Completist, y fuera de ella no responde. Crea un
administrador en **Ajustes > Usuarios** para pedir inicio de sesión, y luego añade más usuarios,
administradores o de solo lectura. Las contraseñas se guardan como hashes PBKDF2; los intentos
fallidos se frenan y después se bloquean.

`COMPLETIST_AUTH_USERNAME` y `COMPLETIST_AUTH_PASSWORD` crean un administrador desde el entorno,
útil si olvidas una contraseña. Usa HTTPS (un proxy inverso) si la app es accesible desde internet.

## Ajustes del entorno

| Variable | Por defecto | Para qué sirve |
| --- | --- | --- |
| `COMPLETIST_DATA_DIR` | `data` junto al programa, `/data` en Docker | La base de datos y los ajustes |
| `COMPLETIST_PORT` | `3000` | Puerto de la interfaz web |
| `COMPLETIST_LIBRARIES` | | Carpetas que se añaden como bibliotecas en el primer arranque, separadas por comas |
| `COMPLETIST_WATCH` | `true` | Escanear cuando cambian los archivos; `false` lo desactiva |
| `COMPLETIST_WATCH_INTERVAL_MINUTES` | `2` | Cada cuánto se revisan las carpetas, para las de red que no avisan de los cambios |
| `COMPLETIST_SCAN_INTERVAL_MINUTES` | `360` | Un escaneo completo por si acaso; `0` lo desactiva |
| `COMPLETIST_AUTO_BACKUP` | `true` | Una copia del estado cada semana; `false` lo desactiva |
| `COMPLETIST_ALLOWED_HOSTS` | | Más nombres de host a los que responde mientras no hay usuarios |
| `COMPLETIST_DAT_UPDATE_HOURS` | `168` | Cada cuánto se actualizan los DAT descargados; `0` lo desactiva |
| `COMPLETIST_SCAN_WORKERS` | hasta 4 | Archivos que se leen a la vez |
| `COMPLETIST_DAT_URL` | libretro-database en GitHub | De dónde se descargan los DAT, para usar un espejo |
| `COMPLETIST_COVERS_URL` | thumbnails.libretro.com | De dónde se descargan las imágenes, para usar un espejo |
| `COMPLETIST_AUTH_USERNAME`, `COMPLETIST_AUTH_PASSWORD` | | Un administrador |
| `COMPLETIST_ALLOW_REMOTE_WITHOUT_LOGIN` | | `true` si otro servicio ya protege la app |
| `COMPLETIST_TRUSTED_PROXIES` | | `cloudflare`, o direcciones de proxies, para ver la dirección real de los visitantes |

## API

La interfaz usa una API JSON que también pueden usar tus scripts, con autenticación HTTP básica
cuando hace falta iniciar sesión (para leer basta un usuario de solo lectura). Los endpoints están
en el [README en inglés](README.md#api).

## Plan

1. **Hecho**: el núcleo, los sistemas de cartucho, los DAT, los modos de recuento, la nueva
   interfaz y las portadas guardadas por el servidor.
2. Estadísticas, lista de deseos y exportación a ES-DE y RetroArch.
3. Sistemas de disco: PlayStation, Saturn, Mega CD, Dreamcast, GameCube y Wii, identificando los
   CHD y RVZ por las sumas de sus pistas.
4. Compresión comprobada contra el DAT antes de borrar el original, renombrado a los nombres del
   DAT con deshacer, y el plugin de Switch con el código de Switch Library Manager Web.

## Compilar

Requisitos: [Go](https://go.dev) 1.26 o posterior y [Node.js](https://nodejs.org) 22 o posterior.

```sh
git clone https://github.com/DeLFuS77/completist.git
cd completist
make ui      # la interfaz (Svelte, Tailwind, esbuild)
make build   # build/completist, con la interfaz dentro
make test
```

`make dev` recompila la interfaz con cada cambio y la sirve sin volver a compilar Go.

## Licencia

[GNU AGPL 3.0](LICENSE). En [NOTICE](NOTICE.md) están los componentes y datos que usa. Completist
no incluye juegos ni los descarga nunca: úsalo con copias de juegos que tengas.
