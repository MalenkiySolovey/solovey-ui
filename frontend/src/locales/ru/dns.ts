export default {
  "dns": {
    "capabilityUnavailable": "Транспорт недоступен. Исправьте конфигурацию с учётом причины от сервера:",
    "mdnsSemantics": "mDNS использует multicast на доступных интерфейсах для .local и обратных доменов link-local IPv4/IPv6. Параметры neighbor-domain и prefer-Go локального резолвера здесь не применяются.",
    "mdnsInterfaces": "Multicast-интерфейсы (через запятую)",
    "mdnsInterfaceHint": "Пустой список выбирает доступные интерфейсы. Каждый выбранный интерфейс должен быть включён, поддерживать multicast, иметь подходящий адрес и не быть loopback.",
    "mdnsObserved": "Сейчас обнаружены доступные интерфейсы:",
    "resolvedSemantics": "Linux-сервис этого ядра владеет именем resolve1 в D-Bus. Другой владелец препятствует использованию. Доступность определяется read-only проверкой; при запуске проверяются права и запрашивается имя.",
    "resolvedMissingService": "Выберите существующий resolved-сервис этого ядра. Создайте или исправьте сервис перед сохранением DNS-транспорта.",
    "add": "Добавить DNS сервер",
    "empty": "Нет настроенных DNS серверов",
    "title": "DNS серверы",
    "final": "Итоговый",
    "server": "Сервер",
    "firstServer": "Первый сервер",
    "cacheCapacity": "Вместимость кэша",
    "disableCache": "Отключить кэш",
    "disableExpire": "Отключить истечение",
    "independentCache": "Независимый кэш",
    "reverseMapping": "Обратное отображение",
    "domainStrategy": "Стратегия домена",
    "local": {
      "preferGo": "Предпочитать Go"
    },
    "rule": {
      "add": "Добавить правило DNS",
      "empty": "Нет правил DNS",
      "title": "Правила DNS",
      "inet4Range": "Диапазон IPv4",
      "inet6Range": "Диапазон IPv6",
      "acceptDefault": "Принять резолверы по умолчанию",
      "queryType": "Тип запроса",
      "ipAcceptAny": "Принимать пустой IP",
      "rulesetAcceptEmpty": "Принимать пустой IP CIDR набора правил",
      "action": {
        "title": "Действие",
        "route": "Маршрутизация",
        "routeOptions": "Параметры маршрутизации",
        "reject": "Отклонить",
        "predefined": "Предопределенные",
        "rewriteTtl": "Перезаписать TTL",
        "clientSubnet": "Подсеть клиента",
        "rcode": "Код ответа",
        "rcodes": {
          "noError": "ОК",
          "formerr": "Неверный запрос",
          "servFail": "Сбой сервера",
          "nxDomain": "Не найдено",
          "refused": "Отклонено",
          "notImp": "Не реализовано"
        },
        "answer": "Ответ",
        "ns": "Серверы имён",
        "extra": "Дополнительные"
      }
    }
  }
}
