import { describe, expect, it } from 'vitest';

import type { Device } from '@/types';

import { deviceInputFrom } from './resources';

const device: Device = {
  id: '0b9f6c1e-5a47-4f3e-9c1d-2e8f7a6b5c4d',
  imei: '869247061230011',
  model: 'J16',
  manufacturer: 'Concox',
  protocol: 'gt06',
  firmware: '1.0',
  phoneNumber: '+5511999990000',
  iccid: '89553202100093795330',
  status: 'ONLINE',
  lastSeenAt: '2026-09-30T12:00:00Z',
  apn: 'zap.vivo.com.br',
  apnUser: 'usuario-apn',
  apnPasswordSet: true,
  serverHost: 'rastreio.exemplo.com',
  serverPort: 5023,
  reportIntervalSeconds: 30,
  heartbeatIntervalSeconds: 300,
  commandPasswordSet: true,
  commandOverrides: { ENGINE_RESUME: 'HFYD,***#' },
  notes: 'sob o painel',
  createdAt: '2026-09-01T12:00:00Z',
  updatedAt: '2026-09-01T12:00:00Z',
};

describe('deviceInputFrom', () => {
  it('não reenvia senha nem override: vazio/ausente mantém o que está gravado', () => {
    // Mesmo que uma API antiga ainda mandasse as senhas, o formulário não as copia.
    const legacy = { ...device, apnPassword: 'vazou-apn', commandPassword: 'vazou-cmd' } as Device;
    const input = deviceInputFrom(legacy);

    expect(input.apnPassword).toBe('');
    expect(input.commandPassword).toBe('');
    expect(input).not.toHaveProperty('commandOverrides');
    expect(input).not.toHaveProperty('apnPasswordSet');
    expect(input).not.toHaveProperty('commandPasswordSet');
    expect(JSON.stringify(input)).not.toContain('vazou');
  });

  it('mantém os campos editáveis do cadastro', () => {
    const input = deviceInputFrom(device);
    expect(input).toMatchObject({
      imei: device.imei,
      phoneNumber: device.phoneNumber,
      iccid: device.iccid,
      apn: device.apn,
      apnUser: device.apnUser,
      serverHost: device.serverHost,
      serverPort: device.serverPort,
      reportIntervalSeconds: device.reportIntervalSeconds,
      heartbeatIntervalSeconds: device.heartbeatIntervalSeconds,
      notes: device.notes,
    });
  });
});
