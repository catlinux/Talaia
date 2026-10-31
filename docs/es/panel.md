# Guía del panel web

[Read in English](../en/panel.md) · [Volver al README](../../README.es.md)

Esta guía explica el panel web de Talaia: cómo se organiza, qué hace cada pantalla y cómo llegar a las
acciones que necesitas. Para la instalación y el acceso, consulta [instalacion.md](instalacion.md).
Para las métricas, las reglas de alerta y los canales de notificación, consulta
[monitorizacion.md](monitorizacion.md).

## Contenido

[Estructura](#estructura) · [Resumen](#resumen) · [Servidores](#servidores) · [Página del servidor](#página-del-servidor) ·
[Contenedores y stacks](#contenedores-y-stacks) · [Consejos «¿Qué es?»](#consejos-qué-es) ·
[Alertas](#alertas) · [Paleta de comandos](#paleta-de-comandos) · [Usuarios, Auditoría y Ajustes](#usuarios-auditoría-y-ajustes) ·
[Móvil](#móvil)

## Estructura

La barra lateral se agrupa en **Infraestructura**, **Supervisión** y **Administración**. **Resumen** es
la pantalla de inicio; **Servidores**, **Contenedores** y **Stacks** están en Infraestructura;
**Alertas** y **Auditoría** en Supervisión; **Usuarios** y **Ajustes** en Administración. Algunas
entradas solo aparecen cuando tu cuenta tiene acceso a esa área.

La barra superior muestra la ruta de la página actual, un cuadro **Buscar…** que abre la paleta de
comandos, la campana de alertas y el menú de usuario. El menú de usuario cambia el **Idioma** y el
**Tema**, abre **Acerca de** y cierra la sesión. En un ordenador la barra lateral se puede contraer; en
un teléfono se abre como una capa superpuesta.

Cada pantalla y cada pestaña tienen su propia dirección. Atrás, recargar y los enlaces compartidos
mantienen la misma pantalla abierta, y el panel vuelve a comprobar los permisos al abrir la dirección.

## Resumen

**Resumen** es la flota de un vistazo. La franja superior resume **Servidores**, **Alertas**,
**Actualizaciones de seguridad** y **Reinicios pendientes**. Debajo, **Requiere atención** lista lo que
conviene resolver primero, con una acción por elemento:

- **Abrir la alerta** abre el detalle de una alerta activa crítica o de aviso.
- **Abrir el servidor** abre un servidor sin conexión o con el disco lleno.
- **Ver las actualizaciones** abre la pestaña **Actualizaciones** de un servidor con actualizaciones de
  seguridad pendientes o un reinicio necesario.
- **Actualizar el agente** abre el servidor cuyo agente tiene una actualización disponible.

La misma pantalla incluye una tabla compacta de servidores y la actividad reciente de la flota. Si tu
cuenta puede dar de alta servidores, **Añadir servidor** también está en la cabecera.

## Servidores

**Servidores** es la tabla de la flota, con una vista opcional de **Tarjetas**. El cuadro de búsqueda,
los filtros (**Estado**, **Etiqueta**, **Actualizaciones de seguridad**, **Reinicio pendiente**) y el
orden de la tabla se guardan en la dirección, así que una lista filtrada u ordenada se puede compartir o
recargar. En anchos menores se ocultan columnas secundarias como SO y actualizaciones para que sigan
visibles la columna **Agente** y el menú de fila.

Cada fila muestra el estado, el nombre y el nombre de host, los medidores de CPU, RAM y disco, los
contenedores, las alertas, las actualizaciones y la versión del agente. El menú de fila abre el
servidor, sus contenedores, sus actualizaciones o sus alertas. Un servidor con acceso limitado muestra
solo lo que permiten sus permisos.

Para añadir un servidor, pulsa **Añadir servidor**, ponle un nombre y pulsa **Crear token**. Ejecuta la
orden que aparece en el servidor nuevo en los próximos 15 minutos: el token vale una sola vez y no se
vuelve a mostrar. El servidor aparece en la lista cuando su agente se da de alta. Consulta
[instalacion.md](instalacion.md#4-añadir-un-servidor).

## Página del servidor

Al abrir un servidor aparece una cabecera fija con su nombre, estado, uso de CPU, RAM y disco y sus
acciones principales. **Ver las alertas** abre la bandeja de alertas filtrada por ese servidor;
**Copiar el ID del servidor** copia el identificador que usan la API y la CLI. Si el agente se puede
actualizar, la cabecera ofrece la acción de actualización. **Terminal** abre la shell de root grabada
cuando el agente lo permite.

Las pestañas son **Resumen**, **Contenedores**, **Servicios**, **Registros**, **Actualizaciones** y
**Alertas**. La cabecera y las pestañas se quedan en su sitio mientras se desplaza el contenido de la
pestaña elegida. Cada pestaña también es una dirección directa, por ejemplo
`/servers/<id>/updates`, y una pestaña se oculta cuando tu cuenta no puede usarla.

## Contenedores y stacks

**Contenedores** y **Stacks** son listas de toda la flota, entre todos los servidores. Las dos ofrecen
búsqueda, filtros por servidor y estado, agrupación y una tabla con los datos importantes. La lista de
contenedores también muestra el uso de CPU y memoria; la de stacks, cuántos contenedores están en
marcha.

Según la fila, su menú ofrece acciones como **Iniciar**, **Detener**, **Reiniciar**, **Eliminar**,
**Registros** o **Terminal**. Selecciona varias filas para usar las acciones en bloque. Las acciones
peligrosas piden confirmación: eliminar un contenedor conserva sus volúmenes, detener un stack detiene
sus contenedores y la confirmación explica el efecto antes de continuar. Para adoptar un proyecto
Compose que ya está en marcha, usa **Adoptar** en **No gestionadas**; Talaia lo gestiona donde está, sin
copiarlo ni reiniciarlo.

Dentro de un servidor, **Contenedores** también lista los stacks gestionados. Cada stack tiene su propia
página con sus contenedores, registros y acciones, y **Nuevo stack** abre el editor de Compose. Un
archivo Compose se ejecuta como root en el servidor, así que da acceso solo a personas de confianza.

## Consejos «¿Qué es?»

El botón ⓘ que está junto a un contenedor, un stack o un servicio abre una ficha de referencia breve. Su
nombre accesible es **Más información**. La ficha muestra la categoría cuando se conoce y una etiqueta
de importancia: **Esencial**, **Importante**, **Opcional** o **Desconocida**. Después explica **Qué es**,
**Si falla** y, cuando es útil, el **Origen**, por ejemplo la unidad de systemd o el stack y la imagen de
un contenedor.

Los servicios e imágenes conocidos usan el catálogo integrado; cuando Talaia no conoce uno, la ficha
muestra igualmente su origen o, en un servicio desconocido, la descripción que informa systemd. Consulta
la ficha antes de detener algo: indica si el servidor o tu acceso dependen de ello y qué revisar primero.

## Alertas

**Alertas** se abre en **Activas**, la bandeja de alertas actuales. Las demás pestañas son **Historial**,
**Silencios** y, para administradores, **Reglas**, **Comprobaciones** y **Canales**. La bandeja marca
cada alerta como **Nueva**, **Vista** o **Silenciada**. La búsqueda y los filtros **Gravedad**,
**Estado**, **Servidor** y **Regla** acotan la lista; **Agrupar por** puede agruparla por **Servidor** o
**Regla**, y **Limpiar filtros** la reinicia.

Selecciona filas para **Confirmar**, **Silenciar** o **Dejar de vigilar** en bloque. La barra explica
qué alertas seleccionadas no se pueden procesar y por qué, y las acciones en bloque peligrosas piden
confirmación. Al abrir una fila aparece el panel de detalle: **Dónde** ocurrió, **Acciones** sobre su
causa (por ejemplo **Abrir servidor**, **Iniciar contenedor**, **Reiniciar contenedor**, **Ver
registros**, **Abrir terminal**, **Abrir stack**, **Abrir servicios** o
**Limpiar recursos**), la **Cronología**, la **Regla** con su umbral y un enlace **Editar regla**, y la
gráfica de la **Métrica** alrededor del incidente. **Confirmar** detiene las notificaciones repetidas;
**Silenciar** pide una **Duración** y un **Motivo** y suprime el envío sin ocultar el incidente; **Dejar
de vigilar** deja de vigilar un contenedor hasta que vuelva a ejecutarse.

La campana de la barra superior muestra el número de alertas abiertas y las últimas nuevas o críticas.
**Ver todas las alertas** abre la bandeja. Solo las cuentas con `alerts.manage` ven **Reglas**,
**Comprobaciones** y **Canales**: **Reglas** controla qué genera una alerta, **Comprobaciones** gestiona
sondas HTTP, TCP y TLS con **Ejecutar ahora**, y **Canales** configura Telegram, ntfy, correo, Discord y
webhooks con **Enviar prueba**.

## Paleta de comandos

Pulsa **Ctrl K** (o **⌘K** en un Mac), o usa el cuadro **Buscar…** de la barra superior, para abrir la
paleta de comandos. Busca en **Pantallas**, **Recientes**, **Servidores**, **Contenedores**, **Stacks** y
**Acciones**. Las acciones incluyen **Añadir servidor**, **Cambiar tema**, **Cambiar idioma** y **Cerrar
sesión**. Usa las flechas para moverte, **Intro** para abrir el resultado seleccionado y **Esc** para
cerrar la paleta.

## Usuarios, Auditoría y Ajustes

**Usuarios** está disponible para administradores. **Personas**, **Equipos** y **Grupos** cubren las
cuentas, la pertenencia a equipos y los grupos de servidores. Desde aquí puedes invitar a un usuario,
editar una cuenta, activarla o desactivarla, eliminarla y asignarle un rol y un ámbito. Las
descripciones de los roles y las reglas de acceso están en [permisos.md](permisos.md).

**Auditoría** muestra las pestañas **Registro de auditoría** y **Grabaciones** cuando tu cuenta tiene
esos permisos. El registro lista todas las acciones registradas, de la más reciente a la más antigua, y
se puede filtrar por grupo de acciones, servidor y actor o buscar por texto. **Detalles** abre el panel
del evento; las grabaciones de terminal se descargan desde **Grabaciones**.

**Ajustes** agrupa **Cuenta**, **Servidores**, **Integraciones** y **Sistema**. Cuenta contiene los
dispositivos de confianza y las claves de acceso; Servidores muestra las versiones de los agentes,
permite actualizarlos y elimina un servidor tras escribir su nombre; Integraciones contiene las claves
de API y el inicio de sesión único; Sistema comprueba la versión, lista los commits pendientes y aplica
las actualizaciones del panel. Las pestañas aparecen solo cuando tu cuenta puede usarlas.

## Móvil

En un teléfono la barra lateral se convierte en una capa superpuesta que se abre con el botón de menú.
La ruta de navegación se oculta y el cuadro de búsqueda pasa a ser un icono que abre la misma paleta de
comandos. Las tablas se convierten en tarjetas con etiquetas, las barras de pestañas anchas se desplazan
o se envuelven y los formularios apilan sus campos. Los menús de fila, las acciones en bloque, las
confirmaciones, los avisos y la campana de alertas siguen funcionando. Para una revisión rápida, empieza
por **Resumen** y **Alertas**; los paneles de detalle y la terminal se pueden usar, pero necesitan más
desplazamiento.
