// Both doors use the same provider selection rule. An operator token is an
// alternative and must remain reachable rather than silently redirected.
export function singleProviderDestination(auth:{providers:{id:string}[];password?:boolean|{id:string};token:boolean}, search:string,next:string):string|null {
 const query=new URLSearchParams(search);
 if(auth.providers.length!==1 || auth.password || auth.token || query.has('login_error') || query.has('error') || query.has('manual'))return null;
 return `/api/auth/login/${encodeURIComponent(auth.providers[0].id)}?next=${encodeURIComponent(next)}`;
}
