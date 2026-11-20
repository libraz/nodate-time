import { describe, expect, it } from 'vitest';
import type { Member } from '@/types/calendar';
import { memberOption } from './member-option';

const t = (key: string, params?: Record<string, string | number>) =>
  params ? `${key}:${params.name}` : key;

const member: Member = {
  id: 'u1',
  name: 'Hanako',
  role: 'editor',
  color: '#ff0000',
  avatarUrl: 'https://example.test/a.png',
};

describe('memberOption', () => {
  it('carries the colour and the person for the avatar', () => {
    expect(memberOption(member, 'other', t)).toEqual({
      value: 'u1',
      label: 'Hanako',
      color: '#ff0000',
      person: { name: 'Hanako', avatarUrl: 'https://example.test/a.png' },
    });
  });

  it('marks the viewer own row', () => {
    expect(memberOption(member, 'u1', t).label).toBe('event.memberMe:Hanako');
  });

  it('does not mark anyone without a viewer', () => {
    expect(memberOption(member, undefined, t).label).toBe('Hanako');
  });
});
