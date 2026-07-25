import { validate } from '../../shared/validate';

export function getUser(id: string): string {
  if (!validate(id)) return '';
  return `user-${id}`;
}
