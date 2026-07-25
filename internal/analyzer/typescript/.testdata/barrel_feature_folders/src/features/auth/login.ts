import { validate } from '../../shared/validate';

export function login(email: string, password: string): boolean {
  return validate(email) && password.length > 0;
}
