export async function exercisePackagedBrowser(context){
 const assertions=[],scopes=[]
 // Exact full package Naive49 passed in38182097918 and remains separately sealed.
 // Core Naive was not reached there. Only failed/unrun scopes are executed here.
 const required=process.env.W5_NATIVE_PROFILE==='full'?['autohttps','outbound-mtls']:['naive','autohttps','outbound-mtls']
 for(const name of required){const module=await import('./'+name+'-contract.mjs');const result=await module.exercisePackagedBrowser(context);if(result.status!=='PASS')throw Error('NATIVE_SCOPE_NOT_PASS');assertions.push(...result.assertions.map(x=>name+'_'+x));scopes.push({name,...result})};return{status:'PASS',assertions,scopes,retainedFullNaiveRun:process.env.W5_NATIVE_PROFILE==='full'?38182097918:null};
}
