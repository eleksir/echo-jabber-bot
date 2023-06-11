# Echo Jabber Bot

Простой бот, работающий по протоколу xmpp. Его задача - демонстрация возможностей библиотеки github.com/mattn/go-xmpp.

На его основе (будет?) построен xmpp-фронтэнд бота aleesa-bot.

## Что он может?

Может повторять все сказанные в чят сообщения. Умеет работать с приватными и публичными чатами.

Имеет возможность соединяться с сервером по протоколу xmpp как с шифрованием, так и без. Шифрование работает по
классическим механикам (шифрованный канал связи, без возможности коммуникации в незашифрованном виде), без поддержки
механизма start tls. Что-то в гошке или на стороне сервера при выборе StartTLS не работает.

## Что он не может?

* Не может заходить в комнаты, защищённые паролем.
* Не умеет разгадывать капчу.

## Как заставить его работать?

Не надо этого делать. Он уже работает, потому что существует.

## Дисклеймер

Весь дисклеймер в файле LICENSE.txt :)