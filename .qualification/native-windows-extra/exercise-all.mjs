export async function exercisePackagedBrowser(context){
 const assertions=[],scopes=[];for(const name of ['naive','autohttps','outbound-mtls']){const module=await import('./'+name+'-contract.mjs');const result=await module.exercisePackagedBrowser(context);if(result.status!=='PASS')throw Error('NATIVE_SCOPE_NOT_PASS');assertions.push(...result.assertions.map(x=>name+'_'+x));scopes.push({name,...result})};return{status:'PASS',assertions,scopes};
}
