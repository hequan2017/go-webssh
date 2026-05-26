jQuery(function ($) {

    var status = $('#status');

    $('form#connect').submit(function (event) {
        event.preventDefault();

        var form = $(this),
            url = form.attr('action'),
            type = form.attr('type');

        var hostname = form.find('input[name="hostname"]').val();
        var port = form.find('input[name="port"]').val() || '22';
        var username = form.find('input[name="username"]').val();
        var password = form.find('input[name="password"]').val();

        $.ajax({
            url: url,
            type: type,
            data: {hostname: hostname, port: port, username: username, password: password},
            success: callback
        });
    });

    function callback(msg) {
        console.log(msg);
        if (msg.status) {
            status.text(msg.status);
            return;
        }

        var wsProtocol = window.location.protocol === 'https:' ? 'wss://' : 'ws://';
        var url = wsProtocol + window.location.host + '/ws?id=' + msg.id,
            socket = new WebSocket(url),
            terminal = document.getElementById('#terminal'),
            term = new Terminal({cursorBlink: true});

        term.on('data', function (data) {
            socket.send(data);
        });

        socket.onopen = function () {
            $('.container').hide();
            term.open(terminal, true);
        };

        socket.onmessage = function (msg) {
            term.write(msg.data);
        };

        socket.onerror = function (e) {
            console.error(e);
        };

        socket.onclose = function () {
            term.destroy();
            $('.container').show();
        };
    }
});
