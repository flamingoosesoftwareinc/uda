import { User } from '../models/user';

export class UserService {
  getUser(): User {
    return { name: 'test' };
  }
}
