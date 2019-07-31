<template>
    <div>
        <Row>
            <Card>

                <br />
                <div class="container">
                    <my-terminal :terminal="terminal"
                                 :url="url"></my-terminal>
                </div>

            </Card>
        </Row>

    </div>
</template>

<script>
    import Console from 'Console'
    export default {
        data () {
            return {
                title: 'webssh',
                name: '',
                namespace: '',
                terminal: {
                    pid: 1,
                    name: 'terminal',
                    cols: 400,
                    rows: 400
                },
                url: `ws://192.168.42.1:8080/ws/${this.$route.params.name}?cols=188&rows=50`
            }
        },
        methods: {
            connection () {
                console.log(this.$route.params.name)
            }
        },
        components: {
            'my-terminal': Console
        }
    }
</script>
