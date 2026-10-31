# Acceso e inicio de sesión

[Read in English](../en/access.md) · [Llegir en català](../ca/access.md) · [Volver al README](../../README.es.md)

Talaia siempre parte de una contraseña y un código TOTP (las seis cifras de una aplicación de autenticación),
con los códigos de recuperación como vía de vuelta. Sobre esa base puedes añadir dos métodos más rápidos:

- un **PIN en un dispositivo de confianza**, para un navegador que uses habitualmente;
- **claves de acceso** (passkeys), que usan la huella, la cara o el PIN del dispositivo mediante WebAuthn.

Los dos son opcionales. La contraseña + TOTP y los códigos de recuperación siguen funcionando, y siguen siendo
la vía de recuperación si se pierde un teléfono, un navegador o una aplicación de autenticación. El diseño está
en [seguridad.md](seguridad.md); los pasos de instalación, en [instalacion.md](instalacion.md).

## PIN en un dispositivo de confianza

Un dispositivo de confianza es un navegador que puede abrir una sesión completa con un PIN. No es una segunda
cuenta de administrador ni sustituye a la contraseña en todas partes: otro navegador sigue entrando con la
contraseña y TOTP.

### Cómo funciona

Cuando confías en un navegador, el núcleo le envía una cookie de larga duración `__Host-talaia_device` (180
días, `HttpOnly`, `Secure`, `SameSite=Strict`) y guarda solo su hash. El navegador también conserva el nombre
del dispositivo, que no es secreto, en `localStorage` para poder mostrar el formulario del PIN. El PIN se guarda
con hash, nunca en claro.

El PIN no sirve sin esa cookie. Copiar el PIN a otro navegador no funciona, y borrar la cookie hace que el
navegador vuelva a pedir la contraseña.

El segundo factor no desaparece: se recuerda durante un tiempo. Se vuelve a pedir el TOTP cuando se ha cerrado
el navegador (la cookie marcadora `__Host-talaia_browser` es de sesión) o cuando han pasado más de
`device_mfa_hours` desde el último TOTP de ese dispositivo. El valor por defecto es 12 horas; el rango va de 1
a 168 horas. Se configura con `TALAIA_DEVICE_MFA_HOURS` en el entorno o `device_mfa_hours` en el fichero TOML.

«Cerrado» significa que el navegador ha descartado sus cookies de sesión. Los navegadores del móvil lo hacen al
cerrar la app; Chrome en el ordenador las conserva si está activado **Continuar donde lo dejaste** o si sigue
**ejecutándose en segundo plano al cerrarlo** (Configuración > Sistema), y entonces solo cuenta el límite de
tiempo. Si quieres el segundo factor en cada inicio del navegador, desactiva ambos ajustes o baja
`device_mfa_hours`.

Cinco PIN erróneos desactivan el dispositivo: se borra su PIN y hay que volver a añadirlo. Un usuario puede
tener como máximo 10 dispositivos de confianza activos.

### Configurarlo

1. Entra con la contraseña y TOTP.
2. Abre **Ajustes > Cuenta**.
3. En **Dispositivos de confianza**, rellena **Nombre del dispositivo** (por ejemplo, el nombre del portátil),
   **Contraseña actual**, **PIN** y **Confirmar PIN**.
4. Pulsa **Confiar en este dispositivo**.

El PIN debe tener de 6 a 12 dígitos. No puede usar el mismo dígito repetido ni ser una secuencia seguida
ascendente o descendente (`123456` o `654321`, por ejemplo). La contraseña actual se comprueba otra vez al
crear el dispositivo.

### Usarlo

La próxima vez que abras el panel en ese navegador, el formulario del PIN será el principal. Escribe el PIN y
pulsa **Entra con PIN**:

- si la cookie marcadora y la ventana de 12 horas siguen vigentes, la sesión se abre directamente;
- si no, el panel pide el código TOTP y después continúa con normalidad.

**Usar la contraseña** está siempre disponible. Abre el formulario habitual sin eliminar el dispositivo.

### Quitarlo

Abre **Ajustes > Cuenta > Dispositivos de confianza** y pulsa **Elimina** junto al dispositivo. Si es el
navegador actual, el panel también olvida el nombre local del dispositivo; el siguiente acceso empieza por la
contraseña.

Borrar las cookies o los datos del sitio en el navegador también elimina la cookie del dispositivo en ese
navegador, pero no borra el dispositivo de la cuenta: elimínalo también en Ajustes. Cambiar la contraseña, o
restablecer el secreto TOTP, elimina todos los dispositivos de confianza automáticamente.

### Notas de seguridad

- La cookie del dispositivo es una credencial de acceso para el formulario del PIN. Usa bloqueo de pantalla en
  el dispositivo del navegador y elimina un dispositivo de confianza en cuanto se pierda, se venda o se comparta.
- El PIN protege el paso local, no la cuenta por sí solo. En un navegador nuevo siguen haciendo falta la
  contraseña y el TOTP.
- Un PIN erróneo no revela si falló el dispositivo o el PIN, y cinco fallos desactivan el dispositivo.
- Como máximo puede haber 10 dispositivos activos por usuario. Si se llega al límite, elimina uno antes.

## Claves de acceso

Una clave de acceso es una credencial WebAuthn guardada por el navegador o el sistema operativo: la huella, la
cara o el PIN del dispositivo la desbloquean. Talaia exige verificación del usuario, así que el autenticador
debe confirmar a la persona antes de firmar el desafío.

### Cómo funciona

Una clave de acceso cuenta como la contraseña y el segundo factor a la vez. Tras un acceso correcto con clave,
Talaia abre una sesión completa y no pide TOTP. La contraseña + TOTP y los códigos de recuperación siguen
disponibles para los casos en que no se pueda usar la clave.

Un usuario puede tener como máximo 10 claves de acceso. Cada desafío de registro y cada desafío de acceso
caduca a los cinco minutos. Si el contador de firmas del autenticador retrocede, Talaia lo interpreta como un
posible autenticador clonado y desactiva esa clave; vuelve a añadirla solo después de revisar el dispositivo.

### Requisitos

Las claves de acceso necesitan HTTPS con un nombre DNS en `public_hostnames` (`TALAIA_PUBLIC_HOSTNAMES` en
`.env`, o `public_hostnames` en el fichero TOML). Una dirección IP no puede ser el identificador de confianza
(relying-party ID) de WebAuthn, así que un despliegue configurado solo con IP no ofrece claves de acceso.

Con la CA propia de Talaia, algunos navegadores se niegan a crear o usar claves de acceso. Confía en la CA en
el dispositivo o instala el panel con un certificado en el que confíe el navegador.

### Configurarlas

1. Entra con la contraseña y TOTP.
2. Abre **Ajustes > Cuenta**.
3. En **Claves de acceso**, pulsa **Añadir una clave de acceso**.
4. Escribe un **Nombre de la clave de acceso** y tu **Contraseña actual**.
5. Confirma el aviso del navegador con la huella, la cara o el PIN del dispositivo.

Si el despliegue no admite claves de acceso, la tarjeta muestra **Las claves de acceso necesitan HTTPS con un
nombre de dominio** y no enseña el formulario.

### Usarlas

En la pantalla de acceso, pulsa **Entra con la huella o el PIN de este dispositivo**. Elige la clave si el
navegador lo pregunta y confirma con el autenticador. Cancelar el aviso te deja en la pantalla de acceso sin
error; el formulario de contraseña sigue ahí.

### Quitarlas

Abre **Ajustes > Cuenta > Claves de acceso** y pulsa **Elimina** junto a la clave. La clave privada se queda en
el autenticador; eliminar la clave solo borra el registro de clave pública de Talaia, así que el mismo
autenticador se puede volver a registrar más adelante. Elimina una clave que ya no controles antes de añadir su
sustituta.

Si pierdes todas las claves de acceso, entra con la contraseña, el TOTP y, si hace falta, un código de
recuperación; después añade una clave nueva desde Ajustes.

### Notas de seguridad

- Talaia nunca recibe la clave privada; guarda la clave pública y comprueba la firma contra el dominio del
  panel.
- Una clave de acceso está ligada al sitio para el que se creó, lo que la hace resistente al phishing de
  contraseñas. Talaia sigue exigiendo el PIN o la comprobación biométrica del dispositivo (verificación del
  usuario).
- Conserva al menos otra forma de entrar: la contraseña + TOTP y los códigos de recuperación en un sitio seguro.
- Una clave de acceso no se salta el bloqueo de la cuenta ni el registro de auditoría: los accesos con clave
  quedan registrados como los accesos con contraseña.

## Problemas frecuentes

| Síntoma | Causa y solución |
|---|---|
| No aparece el formulario del PIN | Este navegador no tiene un dispositivo recordado, se borró o caducó la cookie del dispositivo, se cambió la contraseña o se eliminó el dispositivo. Entra con la contraseña y vuelve a confiar en el navegador. |
| El panel pide TOTP aunque el PIN funcione | Se cerró el navegador o caducó la ventana de `device_mfa_hours`. Es lo esperado; escribe el código TOTP y el formulario del PIN volverá en el siguiente acceso. |
| El PIN se rechaza y el formulario desaparece | Cinco PIN erróneos desactivan el dispositivo. Entra con la contraseña y TOTP y vuelve a añadirlo con un PIN nuevo. |
| No aparece el botón de la clave de acceso | El navegador no tiene WebAuthn, el despliegue no tiene un nombre DNS HTTPS en `public_hostnames` o se eliminó la clave. Usa contraseña + TOTP y revisa los requisitos de arriba. |
| El aviso de la clave falla o el navegador no confía en el panel | Con la CA propia, algunos navegadores rechazan las claves. Confía en la CA en el dispositivo o usa un certificado en el que confíe el navegador, y vuelve a probar. |
| Una clave de acceso dejó de funcionar | Puede que el autenticador haya informado de un contador de firmas menor que el guardado y Talaia la haya desactivado. Revisa el dispositivo y vuelve a añadir la clave. |
| Se pierde el dispositivo o el autenticador | Usa la contraseña, el TOTP y los códigos de recuperación. Cuando vuelvas a entrar, elimina el dispositivo o la clave perdidos en **Ajustes > Cuenta**. |

## Véase también

- [Seguridad](seguridad.md): qué protege el panel y qué se graba.
- [Instalación](instalacion.md): primer acceso, puertos, agentes y actualizaciones.
