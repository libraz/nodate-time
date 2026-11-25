import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('@/lib/toast', () => ({
  toast: { error: vi.fn(), success: vi.fn(), info: vi.fn() },
}));

vi.mock('@/lib/download', () => ({ startDownload: vi.fn() }));

import { AttachmentsSection } from '@/components/event-attachments';
import { ChecklistSection } from '@/components/event-checklist';
import { CommentsSection } from '@/components/event-comments';
import { toast } from '@/lib/toast';
import { jsonResponse, mockApi } from '@/test/mock-client';

const BASE = '/calendars/cal-1/events/evt-1';

function checklistItem(over: Record<string, unknown> = {}) {
  return {
    id: 'item-1',
    title: 'Pack',
    done: false,
    sortOrder: 0,
    createdAt: '2026-04-20T10:00:00Z',
    etag: '"v1"',
    ...over,
  };
}

function comment(over: Record<string, unknown> = {}) {
  return {
    id: 'c-1',
    authorId: 'u-1',
    authorName: 'Rin',
    body: 'hello',
    createdAt: '2026-04-20T10:00:00Z',
    etag: '"c1"',
    ...over,
  };
}

beforeEach(() => {
  localStorage.clear();
  vi.mocked(toast.error).mockClear();
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

describe('ChecklistSection', () => {
  it('toggles with a PATCH that carries the etag the list returned', async () => {
    const patch = vi.fn((_req: Request) => jsonResponse(checklistItem({ done: true })));
    mockApi({
      [`GET ${BASE}/checklist`]: jsonResponse([checklistItem()]),
      [`PATCH ${BASE}/checklist/item-1`]: patch,
    });
    render(<ChecklistSection calendarId="cal-1" eventId="evt-1" editable />);

    fireEvent.click(await screen.findByRole('button', { name: 'Pack' }));

    await waitFor(() => expect(patch).toHaveBeenCalledTimes(1));
    const req = patch.mock.calls[0]?.[0] as Request;
    expect(req.headers.get('If-Match')).toBe('"v1"');
    expect(await req.json()).toEqual({ done: true });
  });

  it('reports a stale toggle and reads the list again instead of retrying', async () => {
    const list = vi.fn(() => jsonResponse([checklistItem()]));
    const patch = vi.fn(() =>
      jsonResponse({ status: 412, code: 'CHECKLIST.STALE', message: 'stale' }, 412),
    );
    mockApi({
      [`GET ${BASE}/checklist`]: list,
      [`PATCH ${BASE}/checklist/item-1`]: patch,
    });
    render(<ChecklistSection calendarId="cal-1" eventId="evt-1" editable />);

    fireEvent.click(await screen.findByRole('button', { name: 'Pack' }));

    await waitFor(() => expect(list).toHaveBeenCalledTimes(2));
    expect(patch).toHaveBeenCalledTimes(1);
    expect(toast.error).toHaveBeenCalledWith(expect.stringMatching(/./));
  });

  it('deletes with the item etag as If-Match', async () => {
    const del = vi.fn(() => new Response(null, { status: 204 }));
    mockApi({
      [`GET ${BASE}/checklist`]: jsonResponse([checklistItem()]),
      [`DELETE ${BASE}/checklist/item-1`]: del,
    });
    render(<ChecklistSection calendarId="cal-1" eventId="evt-1" editable />);

    fireEvent.click(await screen.findByRole('button', { name: /delete|削除/i }));

    await waitFor(() => expect(del).toHaveBeenCalledTimes(1));
    const req = (del.mock.calls[0] as unknown as [Request])[0];
    expect(req.headers.get('If-Match')).toBe('"v1"');
  });
  it('adds an item from the button, with no return key involved', async () => {
    const post = vi.fn((_req: Request) => jsonResponse(checklistItem({ title: 'Minutes' }), 201));
    mockApi({
      [`GET ${BASE}/checklist`]: jsonResponse([]),
      [`POST ${BASE}/checklist`]: post,
    });
    render(<ChecklistSection calendarId="cal-1" eventId="evt-1" editable />);

    const box = await screen.findByRole('textbox');
    const add = screen.getByRole('button', { name: /add item|項目を追加/i });
    expect(add).toHaveProperty('disabled', true);
    fireEvent.change(box, { target: { value: 'Minutes' } });
    fireEvent.click(add);

    await waitFor(() => expect(post).toHaveBeenCalledTimes(1));
    const req = post.mock.calls[0]?.[0] as Request;
    expect(await req.json()).toEqual({ title: 'Minutes', sortOrder: 0 });
  });

  it('leaves the Enter that commits an IME conversion to the IME', async () => {
    const post = vi.fn(() => jsonResponse(checklistItem(), 201));
    mockApi({
      [`GET ${BASE}/checklist`]: jsonResponse([]),
      [`POST ${BASE}/checklist`]: post,
    });
    render(<ChecklistSection calendarId="cal-1" eventId="evt-1" editable />);

    const box = await screen.findByRole('textbox');
    fireEvent.change(box, { target: { value: 'ぎじろく' } });
    fireEvent.keyDown(box, { key: 'Enter', isComposing: true });
    fireEvent.keyDown(box, { key: 'Enter', keyCode: 229 });
    fireEvent.keyDown(box, { key: 'Enter' });

    await waitFor(() => expect(post).toHaveBeenCalledTimes(1));
  });
});

describe('CommentsSection', () => {
  it('reads the {items,nextCursor} page and follows the cursor for older comments', async () => {
    const seen: string[] = [];
    mockApi({
      [`GET ${BASE}/activities`]: (req) => {
        const cursor = new URL(req.url).searchParams.get('cursor');
        seen.push(cursor ?? '');
        return cursor
          ? jsonResponse({ items: [comment({ id: 'c-0', body: 'older' })] })
          : jsonResponse({ items: [comment()], nextCursor: 'cur-1' });
      },
    });
    render(<CommentsSection calendarId="cal-1" eventId="evt-1" canPost moderatorRole="owner" />);

    expect(await screen.findByText('hello')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: /earlier|以前|前/i }));

    expect(await screen.findByText('older')).toBeInTheDocument();
    expect(seen).toEqual(['', 'cur-1']);
  });

  it('posts a comment and expects a created response', async () => {
    const post = vi.fn(() => jsonResponse(comment({ body: 'new' }), 201));
    mockApi({
      [`GET ${BASE}/activities`]: jsonResponse({ items: [] }),
      [`POST ${BASE}/activities`]: post,
    });
    render(<CommentsSection calendarId="cal-1" eventId="evt-1" canPost moderatorRole="owner" />);

    const box = await screen.findByRole('textbox');
    fireEvent.change(box, { target: { value: 'new' } });
    fireEvent.keyDown(box, { key: 'Enter' });

    await waitFor(() => expect(post).toHaveBeenCalledTimes(1));
    const req = (post.mock.calls[0] as unknown as [Request])[0];
    expect(await req.json()).toEqual({ body: 'new' });
  });

  it('leaves the Enter that commits an IME conversion to the IME', async () => {
    const post = vi.fn(() => jsonResponse(comment(), 201));
    mockApi({
      [`GET ${BASE}/activities`]: jsonResponse({ items: [] }),
      [`POST ${BASE}/activities`]: post,
    });
    render(<CommentsSection calendarId="cal-1" eventId="evt-1" canPost moderatorRole="owner" />);

    const box = await screen.findByRole('textbox');
    fireEvent.change(box, { target: { value: 'りょうかい' } });
    fireEvent.keyDown(box, { key: 'Enter', isComposing: true });
    fireEvent.keyDown(box, { key: 'Enter', keyCode: 229 });
    fireEvent.keyDown(box, { key: 'Enter' });

    await waitFor(() => expect(post).toHaveBeenCalledTimes(1));
  });
});

describe('AttachmentsSection', () => {
  it('presigns with a lowercase hex sha256, PUTs the bytes, then confirms', async () => {
    const order: string[] = [];
    let presignBody: Record<string, unknown> = {};
    const fetchMock = mockApi({
      [`GET ${BASE}/attachments`]: jsonResponse([]),
      [`POST ${BASE}/attachments/presign`]: async (req) => {
        order.push('presign');
        presignBody = await req.json();
        return jsonResponse({ attachmentId: 'att-1', uploadUrl: 'http://storage.test/put' }, 201);
      },
      'PUT /put': () => {
        order.push('put');
        return new Response(null, { status: 200 });
      },
      [`POST ${BASE}/attachments/att-1/confirm`]: () => {
        order.push('confirm');
        return jsonResponse({
          id: 'att-1',
          filename: 'a.txt',
          contentType: 'text/plain',
          byteSize: 3,
          createdAt: '2026-04-20T10:00:00Z',
        });
      },
    });
    const { container } = render(
      <AttachmentsSection calendarId="cal-1" eventId="evt-1" editable />,
    );
    await waitFor(() => expect(fetchMock).toHaveBeenCalled());

    const input = container.querySelector('input[type=file]') as HTMLInputElement;
    const file = new File(['abc'], 'a.txt', { type: 'text/plain' });
    fireEvent.change(input, { target: { files: [file] } });

    await waitFor(() => expect(order).toEqual(['presign', 'put', 'confirm']));
    // SHA-256("abc")
    expect(presignBody).toMatchObject({
      sha256: 'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad',
      byteSize: 3,
      filename: 'a.txt',
    });
  });

  it('skips the PUT when the bytes are already stored', async () => {
    const put = vi.fn(() => new Response(null, { status: 200 }));
    const confirm = vi.fn(() => jsonResponse({}));
    mockApi({
      [`GET ${BASE}/attachments`]: jsonResponse([]),
      [`POST ${BASE}/attachments/presign`]: jsonResponse(
        { attachmentId: 'att-1', uploadUrl: '' },
        201,
      ),
      'PUT /put': put,
      [`POST ${BASE}/attachments/att-1/confirm`]: confirm,
    });
    const { container } = render(
      <AttachmentsSection calendarId="cal-1" eventId="evt-1" editable />,
    );

    const input = container.querySelector('input[type=file]') as HTMLInputElement;
    fireEvent.change(input, { target: { files: [new File(['abc'], 'a.txt')] } });

    await waitFor(() => expect(confirm).toHaveBeenCalledTimes(1));
    expect(put).not.toHaveBeenCalled();
  });
});
