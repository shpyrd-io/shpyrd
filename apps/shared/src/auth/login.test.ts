import {describe,it,expect} from 'vitest';
import {singleProviderDestination as destination, switchAccountDestination as switchTo} from './login';
const auth={providers:[{id:'company'}],password:false,token:false};
describe('single-provider login',()=>{
 it('preserves the destination and uses the provider launcher',()=>{expect(destination(auth,'','/invite/code')).toBe('/api/auth/login/company?next=%2Finvite%2Fcode');});
 it('keeps choices and operator access visible',()=>{for(const cfg of [{...auth,password:true},{...auth,token:true},{...auth,providers:[]},{...auth,providers:[{id:'company'},{id:'accounts'}]}])expect(destination(cfg,'','/')).toBeNull();});
 it('does not retry a failed, cancelled, or explicitly manual login',()=>{for(const query of ['?login_error=denied','?error=cancelled','?manual'])expect(destination(auth,query,'/')).toBeNull();});
});
describe('signing in with another account',()=>{
 const accounts={id:'shpyrd-accounts',switchAccountURL:'https://accounts.example/switch-account'};
 it('after a refusal, ends the session at the provider and comes back to its sign-in',()=>{expect(switchTo(accounts,'?login_error=no+access','https://acme.example','/.shpyrd/gate?name=billing')).toBe('https://accounts.example/switch-account?return_to='+encodeURIComponent('https://acme.example/api/auth/login/shpyrd-accounts?next=%2F.shpyrd%2Fgate%3Fname%3Dbilling'));});
 it('is offered only after a refusal',()=>{expect(switchTo(accounts,'','https://acme.example','/')).toBeNull();expect(switchTo(accounts,'?manual','https://acme.example','/')).toBeNull();});
 it('needs a provider page, over https',()=>{expect(switchTo({id:'company'},'?login_error=x','https://acme.example','/')).toBeNull();expect(switchTo({id:'x',switchAccountURL:'http://accounts.example/switch-account'},'?login_error=x','https://acme.example','/')).toBeNull();expect(switchTo({id:'x',switchAccountURL:'javascript:alert(1)'},'?login_error=x','https://acme.example','/')).toBeNull();});
});
