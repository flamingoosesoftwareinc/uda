import { capitalize } from './utils';

export class UserService {
  getName(): string {
    return capitalize('world');
  }
}
