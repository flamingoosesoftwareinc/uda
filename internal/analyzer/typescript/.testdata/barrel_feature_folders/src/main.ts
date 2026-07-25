import { login } from './features/auth';
import { getUser } from './features/users';

login('test@test.com', 'pass');
const user = getUser('123');
