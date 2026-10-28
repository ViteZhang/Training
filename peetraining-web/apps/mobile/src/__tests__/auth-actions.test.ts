import { logout } from '../features/auth/actions';
import { useSession } from '../lib/session';
import { queryClient } from '../lib/queryClient';

const mockPost = jest.fn();
jest.mock('../lib/api', () => ({ api: { POST: (...args: unknown[]) => mockPost(...args) } }));

beforeEach(() => {
  mockPost.mockReset();
  useSession.getState().clear();
  queryClient.clear();
});

it('clears local auth and query state without waiting for server revocation', async () => {
  let finishRequest!: (value: unknown) => void;
  mockPost.mockReturnValue(new Promise((resolve) => {
    finishRequest = resolve;
  }));
  useSession.getState().setSession({
    accessToken: 'access',
    accessExpiresAt: '2099-01-01T00:00:00Z',
    refreshToken: 'refresh',
    refreshExpiresAt: '2099-01-02T00:00:00Z',
  });
  queryClient.setQueryData(['me'], { id: 1 });

  const done = logout();
  expect(useSession.getState().session).toBeUndefined();
  expect(queryClient.getQueryData(['me'])).toBeUndefined();
  await done;
  expect(mockPost).toHaveBeenCalledWith('/auth/logout');

  finishRequest({});
});
