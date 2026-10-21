/**
 * Conversion helpers between the JSON shapes of the core passkey endpoints and
 * the ArrayBuffer shapes of the WebAuthn browser API.
 */

/** JsonCredentialDescriptor is a credential descriptor with a base64url id. */
export interface JsonCredentialDescriptor extends Omit<PublicKeyCredentialDescriptor, 'id'> {
  id: string
}

/** PublicKeyCredentialCreationOptionsJSON is the registration options shape. */
export interface PublicKeyCredentialCreationOptionsJSON {
  publicKey: {
    challenge: string
    rp: PublicKeyCredentialRpEntity
    user: Omit<PublicKeyCredentialUserEntity, 'id'> & { id: string }
    pubKeyCredParams: PublicKeyCredentialParameters[]
    timeout?: number
    excludeCredentials?: JsonCredentialDescriptor[]
    authenticatorSelection?: AuthenticatorSelectionCriteria
    attestation?: AttestationConveyancePreference
  }
  mediation?: CredentialMediationRequirement
}

/** PublicKeyCredentialRequestOptionsJSON is the assertion options shape. */
export interface PublicKeyCredentialRequestOptionsJSON {
  publicKey: {
    challenge: string
    timeout?: number
    rpId?: string
    allowCredentials?: JsonCredentialDescriptor[]
    userVerification?: UserVerificationRequirement
  }
  mediation?: CredentialMediationRequirement
}

/** RegistrationCredentialJSON is the credential object the server verifies. */
export interface RegistrationCredentialJSON {
  id: string
  rawId: string
  type: PublicKeyCredentialType
  response: {
    clientDataJSON: string
    attestationObject: string
    transports?: string[]
  }
  clientExtensionResults: AuthenticationExtensionsClientOutputs
}

/** AssertionCredentialJSON is the assertion object the server verifies. */
export interface AssertionCredentialJSON {
  id: string
  rawId: string
  type: PublicKeyCredentialType
  response: {
    clientDataJSON: string
    authenticatorData: string
    signature: string
    userHandle?: string
  }
  clientExtensionResults: AuthenticationExtensionsClientOutputs
}

/** base64URLToBuffer decodes an unpadded base64url value into bytes. */
export function base64URLToBuffer(value: string): ArrayBuffer {
  const base64 = value.replace(/-/g, '+').replace(/_/g, '/')
  const padded = base64.padEnd(Math.ceil(base64.length / 4) * 4, '=')
  const binary = atob(padded)
  const bytes = new Uint8Array(binary.length)
  for (let index = 0; index < binary.length; index += 1) {
    bytes[index] = binary.charCodeAt(index)
  }
  return bytes.buffer
}

/** bufferToBase64URL encodes bytes as unpadded base64url. */
export function bufferToBase64URL(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer)
  let binary = ''
  for (const byte of bytes) {
    binary += String.fromCharCode(byte)
  }
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}

/** toCreationOptions converts the registration options to browser options. */
export function toCreationOptions(
  json: PublicKeyCredentialCreationOptionsJSON,
): PublicKeyCredentialCreationOptions {
  const { challenge, user, excludeCredentials, ...rest } = json.publicKey
  return {
    ...rest,
    challenge: base64URLToBuffer(challenge),
    user: { ...user, id: base64URLToBuffer(user.id) },
    excludeCredentials: excludeCredentials?.map((credential) => ({
      ...credential,
      id: base64URLToBuffer(credential.id),
    })),
  }
}

/** toRequestOptions converts the assertion options to browser options. */
export function toRequestOptions(
  json: PublicKeyCredentialRequestOptionsJSON,
): PublicKeyCredentialRequestOptions {
  const { challenge, allowCredentials, ...rest } = json.publicKey
  return {
    ...rest,
    challenge: base64URLToBuffer(challenge),
    allowCredentials: allowCredentials?.map((credential) => ({
      ...credential,
      id: base64URLToBuffer(credential.id),
    })),
  }
}

/** serializeRegistrationCredential converts the browser credential to JSON. */
export function serializeRegistrationCredential(
  credential: PublicKeyCredential,
): RegistrationCredentialJSON {
  const response = credential.response as AuthenticatorAttestationResponse
  return {
    id: credential.id,
    rawId: bufferToBase64URL(credential.rawId),
    type: 'public-key',
    response: {
      clientDataJSON: bufferToBase64URL(response.clientDataJSON),
      attestationObject: bufferToBase64URL(response.attestationObject),
      transports: response.getTransports?.() ?? [],
    },
    clientExtensionResults: credential.getClientExtensionResults(),
  }
}

/** serializeAssertionCredential converts the browser assertion to JSON. */
export function serializeAssertionCredential(
  credential: PublicKeyCredential,
): AssertionCredentialJSON {
  const response = credential.response as AuthenticatorAssertionResponse
  const result: AssertionCredentialJSON = {
    id: credential.id,
    rawId: bufferToBase64URL(credential.rawId),
    type: 'public-key',
    response: {
      clientDataJSON: bufferToBase64URL(response.clientDataJSON),
      authenticatorData: bufferToBase64URL(response.authenticatorData),
      signature: bufferToBase64URL(response.signature),
    },
    clientExtensionResults: credential.getClientExtensionResults(),
  }
  if (response.userHandle !== null) {
    result.response.userHandle = bufferToBase64URL(response.userHandle)
  }
  return result
}

/** passkeysSupported reports whether the browser exposes WebAuthn. */
export function passkeysSupported(): boolean {
  return typeof window !== 'undefined' && typeof window.PublicKeyCredential !== 'undefined'
}

/** isPasskeyCancellation reports whether the user dismissed the authenticator. */
export function isPasskeyCancellation(error: unknown): boolean {
  if (typeof error !== 'object' || error === null || !('name' in error)) {
    return false
  }
  const name = error.name
  return typeof name === 'string' && (name === 'NotAllowedError' || name === 'AbortError')
}
