// Both doors use the same provider selection rule. An operator token is an
// alternative and must remain reachable rather than silently redirected.
export function singleProviderDestination(auth:{providers:{id:string}[];password?:boolean|{id:string};token:boolean}, search:string,next:string):string|null {
 const query=new URLSearchParams(search);
 if(auth.providers.length!==1 || auth.password || auth.token || query.has('login_error') || query.has('error') || query.has('manual'))return null;
 return `/api/auth/login/${encodeURIComponent(auth.providers[0].id)}?next=${encodeURIComponent(next)}`;
}

// After a refused sign-in, signing in again with the same provider brings
// back the same account. A provider that names a switch-account page (shpyrd
// Accounts) ends that session there and returns to this provider's sign-in,
// so the person comes back as whoever they chose. Only after a refusal, and
// only an https page.
export function switchAccountDestination(provider:{id:string;switchAccountURL?:string}, search:string, origin:string, next:string):string|null {
 const query=new URLSearchParams(search);
 if(!provider.switchAccountURL || !(query.has('login_error') || query.has('error')))return null;
 let page:URL;
 try{page=new URL(provider.switchAccountURL);}catch{return null;}
 if(page.protocol!=='https:')return null;
 page.searchParams.set('return_to',`${origin}/api/auth/login/${encodeURIComponent(provider.id)}?next=${encodeURIComponent(next)}`);
 return page.toString();
}
