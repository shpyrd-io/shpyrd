import {describe,it,expect} from 'vitest';
import {singleProviderDestination as destination} from './login';
const auth={providers:[{id:'company'}],password:false,token:false};
describe('single-provider login',()=>{
 it('preserves the destination and uses the provider launcher',()=>{expect(destination(auth,'','/invite/code')).toBe('/api/auth/login/company?next=%2Finvite%2Fcode');});
 it('keeps choices and operator access visible',()=>{for(const cfg of [{...auth,password:true},{...auth,token:true},{...auth,providers:[]},{...auth,providers:[{id:'company'},{id:'accounts'}]}])expect(destination(cfg,'','/')).toBeNull();});
 it('does not retry a failed, cancelled, or explicitly manual login',()=>{for(const query of ['?login_error=denied','?error=cancelled','?manual'])expect(destination(auth,query,'/')).toBeNull();});
});
