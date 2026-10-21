import { afterEach, describe, expect, it } from 'vitest'

import {
  base64URLToBuffer,
  bufferToBase64URL,
  isPasskeyCancellation,
  passkeysSupported,
  serializeAssertionCredential,
  serializeRegistrationCredential,
  toCreationOptions,
  toRequestOptions,
  type PublicKeyCredentialCreationOptionsJSON,
  type PublicKeyCredentialRequestOptionsJSON,
} from './webauthn'

function buffer(...values: number[]): ArrayBuffer {
  return new Uint8Array(values).buffer
}

function bytes(value: ArrayBuffer): number[] {
  return [...new Uint8Array(value)]
}

function mockCredential(rawId: ArrayBuffer, response: object): PublicKeyCredential {
  return {
    id: bufferToBase64URL(rawId),
    rawId,
    type: 'public-key',
    response,
    getClientExtensionResults: () => ({}),
  } as unknown as PublicKeyCredential
}

afterEach(() => {
  Reflect.deleteProperty(window, 'PublicKeyCredential')
})

describe('webauthn helpers', () => {
  it('round-trips base64url bytes, including zero and high bytes', () => {
    const original = buffer(0, 1, 2, 127, 128, 254, 255, 43, 47)
    const encoded = bufferToBase64URL(original)
    expect(encoded).not.toContain('=')
    expect(encoded).not.toContain('+')
    expect(encoded).not.toContain('/')
    expect(bytes(base64URLToBuffer(encoded))).toEqual(bytes(original))
  })

  it('decodes creation options to ArrayBuffers', () => {
    const json: PublicKeyCredentialCreationOptionsJSON = {
      publicKey: {
        challenge: 'AQID',
        rp: { id: 'panel.example.com', name: 'Talaia' },
        user: { id: 'BAUG', name: 'admin', displayName: 'admin' },
        pubKeyCredParams: [{ type: 'public-key', alg: -7 }],
        timeout: 60_000,
        excludeCredentials: [{ type: 'public-key', id: 'BwgJ', transports: ['internal'] }],
        authenticatorSelection: {
          residentKey: 'preferred',
          requireResidentKey: false,
          userVerification: 'required',
        },
        attestation: 'none',
      },
    }

    const options = toCreationOptions(json)
    expect(bytes(options.challenge as ArrayBuffer)).toEqual([1, 2, 3])
    expect(bytes(options.user.id as ArrayBuffer)).toEqual([4, 5, 6])
    expect(bytes(options.excludeCredentials?.[0]?.id as ArrayBuffer)).toEqual([7, 8, 9])
    expect(options.authenticatorSelection?.userVerification).toBe('required')
    expect(options.rp.id).toBe('panel.example.com')
  })

  it('decodes assertion options to ArrayBuffers', () => {
    const json: PublicKeyCredentialRequestOptionsJSON = {
      publicKey: {
        challenge: 'AQID',
        rpId: 'panel.example.com',
        userVerification: 'required',
        allowCredentials: [{ type: 'public-key', id: 'BAUG', transports: ['internal'] }],
      },
    }

    const options = toRequestOptions(json)
    expect(bytes(options.challenge as ArrayBuffer)).toEqual([1, 2, 3])
    expect(bytes(options.allowCredentials?.[0]?.id as ArrayBuffer)).toEqual([4, 5, 6])
    expect(options.rpId).toBe('panel.example.com')
    expect(options.userVerification).toBe('required')
  })

  it('serializes registration credentials', () => {
    const credential = mockCredential(buffer(1, 2), {
      clientDataJSON: buffer(3, 4),
      attestationObject: buffer(5, 6),
      getTransports: () => ['internal'],
    })

    const json = serializeRegistrationCredential(credential)
    expect(json.id).toBe(bufferToBase64URL(buffer(1, 2)))
    expect(json.rawId).toBe(bufferToBase64URL(buffer(1, 2)))
    expect(json.response.clientDataJSON).toBe(bufferToBase64URL(buffer(3, 4)))
    expect(json.response.attestationObject).toBe(bufferToBase64URL(buffer(5, 6)))
    expect(json.response.transports).toEqual(['internal'])
    expect(json.type).toBe('public-key')
  })

  it('serializes assertion credentials with and without a user handle', () => {
    const withHandle = mockCredential(buffer(1, 2), {
      clientDataJSON: buffer(3, 4),
      authenticatorData: buffer(5, 6),
      signature: buffer(7, 8),
      userHandle: buffer(9, 10),
    })
    const json = serializeAssertionCredential(withHandle)
    expect(json.response.authenticatorData).toBe(bufferToBase64URL(buffer(5, 6)))
    expect(json.response.signature).toBe(bufferToBase64URL(buffer(7, 8)))
    expect(json.response.userHandle).toBe(bufferToBase64URL(buffer(9, 10)))

    const withoutHandle = mockCredential(buffer(1, 2), {
      clientDataJSON: buffer(3, 4),
      authenticatorData: buffer(5, 6),
      signature: buffer(7, 8),
      userHandle: null,
    })
    expect(serializeAssertionCredential(withoutHandle).response.userHandle).toBeUndefined()
  })

  it('reports WebAuthn support and user cancellations', () => {
    expect(passkeysSupported()).toBe(false)
    Object.defineProperty(window, 'PublicKeyCredential', {
      value: class {},
      configurable: true,
    })
    expect(passkeysSupported()).toBe(true)

    expect(isPasskeyCancellation(new DOMException('cancelled', 'NotAllowedError'))).toBe(true)
    expect(isPasskeyCancellation(new DOMException('aborted', 'AbortError'))).toBe(true)
    expect(isPasskeyCancellation(new Error('other'))).toBe(false)
    expect(isPasskeyCancellation('no')).toBe(false)
  })
})
