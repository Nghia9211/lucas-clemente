import os



workers = int(os.environ.get('GUNICORN_PROCESSES', '5'))

threads = int(os.environ.get('GUNICORN_THREADS', '100'))

# timeout = int(os.environ.get('GUNICORN_TIMEOUT', '120'))

bind = os.environ.get('GUNICORN_BIND', '0.0.0.0:8080')



forwarded_allow_ips = '*'

secure_scheme_headers = { 'X-Forwarded-Proto': 'https' }