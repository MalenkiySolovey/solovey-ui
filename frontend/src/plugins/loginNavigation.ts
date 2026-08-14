type LoginNavigator = () => unknown | Promise<unknown>

let loginNavigator: LoginNavigator = () => {
  if (typeof window !== 'undefined') window.location.assign('/login')
}

export const configureLoginNavigation = (navigate: LoginNavigator) => {
  loginNavigator = navigate
}

export const navigateToLogin = () => loginNavigator()
