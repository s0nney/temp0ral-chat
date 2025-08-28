document.addEventListener('click', function (e) {
    if (e.target.classList.contains('message-username')) {
        e.preventDefault();

        const tooltip = e.target.querySelector('.user-id-tooltip');
        const chatInput = document.querySelector('input[name="chat_message"]');

        if (tooltip && chatInput) {
            const fullId = tooltip.textContent.replace('ID: ', '');
            const shortId = fullId.substring(0, 8);

            const currentValue = chatInput.value;
            const mention = '@' + shortId + ' ';

            if (!currentValue.endsWith(mention)) {
                if (currentValue && !currentValue.endsWith(' ')) {
                    chatInput.value = currentValue + ' ' + mention;
                } else {
                    chatInput.value = currentValue + mention;
                }
            }

            chatInput.focus();
            chatInput.setSelectionRange(chatInput.value.length, chatInput.value.length);
        }
    }
});

document.addEventListener('htmx:wsAfterMessage', function () {
    const messages = document.getElementById('messages');
    messages.scrollTop = messages.scrollHeight;

    highlightRepliedMessages();
});

document.addEventListener('DOMContentLoaded', function () {
    const chatInput = document.querySelector('input[name="chat_message"]');
    const errorContainer = document.getElementById('error-container');

    if (chatInput && errorContainer) {
        chatInput.addEventListener('focus', function () {
            errorContainer.innerHTML = '';
        });
    }

    // highlightRepliedMessages();
    initializeFilePreview();
    //setupDeleteButtons();
});

/*
function highlightRepliedMessages() {
    const messages = document.querySelectorAll('.message');

    messages.forEach(message => {
        const content = message.textContent;
        const userIdElement = message.querySelector('.user-id-tooltip');

        if (!userIdElement) return;

        const fullUserId = userIdElement.textContent.replace('ID: ', '');
        const shortUserId = fullUserId.substring(0, 4);

        const mentionPattern = new RegExp('@' + shortUserId + '\\b');
        let isRepliedTo = false;

        messages.forEach(otherMessage => {
            if (otherMessage !== message) {
                const otherContent = otherMessage.textContent;
                if (mentionPattern.test(otherContent)) {
                    isRepliedTo = true;
                }
            }
        });

        if (isRepliedTo) {
            message.classList.add('reply-highlight');
        } else {
            message.classList.remove('reply-highlight');
        }
    });
}
    */

function initializeFilePreview() {
    const fileInput = document.getElementById('file-input');
    const filePreview = document.getElementById('file-preview');
    
    if (fileInput && filePreview) {
        fileInput.addEventListener('change', function(e) {
            const file = e.target.files[0];
            
            if (file) {
                if (file.size > 5 * 1024 * 1024) {
                    alert('File too large! Maximum size is 5MB.');
                    fileInput.value = '';
                    clearFilePreview();
                    return;
                }
                
                if (!file.type.startsWith('image/')) {
                    alert('Please select an image file.');
                    fileInput.value = '';
                    clearFilePreview();
                    return;
                }
                
                const reader = new FileReader();
                reader.onload = function(e) {
                    filePreview.innerHTML = `
                        <div class="file-preview-item">
                            <img src="${e.target.result}" alt="Preview" class="file-preview-image">
                            <div class="file-preview-info">
                                <div class="file-preview-name">${file.name}</div>
                                <div class="file-preview-size">${(file.size / 1024 / 1024).toFixed(2)} MB</div>
                            </div>
                            <button type="button" class="file-remove" onclick="clearFilePreview()">Remove</button>
                        </div>
                    `;
                    filePreview.classList.add('active');
                };
                reader.readAsDataURL(file);
            } else {
                clearFilePreview();
            }
        });
    }
}

function clearFilePreview() {
    const fileInput = document.getElementById('file-input');
    const filePreview = document.getElementById('file-preview');
    
    if (fileInput) fileInput.value = '';
    if (filePreview) {
        filePreview.innerHTML = '';
        filePreview.classList.remove('active');
    }
}

function scrollToBottom() {
    const messagesContainer = document.getElementById('messages');
    if (messagesContainer) {
        messagesContainer.scrollTop = messagesContainer.scrollHeight;
    }
}

document.body.addEventListener('htmx:afterSwap', function(evt) {
    if (evt.target && evt.target.id === 'messages') {
        scrollToBottom();
        highlightRepliedMessages();
    }
});

document.addEventListener('click', function(e) {
    if (e.target.matches('.message-image img')) {
        const img = e.target;
        const fullSizeWindow = window.open('', '_blank');
        fullSizeWindow.document.write(`
            <html>
                <head>
                    <title>Image Preview</title>
                    <style>
                        body { 
                            margin:0; 
                            padding:0; 
                            background:#000; 
                            display:flex; 
                            justify-content:center; 
                            align-items:center; 
                            min-height:100vh; 
                        }
                        img { 
                            max-width:100%; 
                            max-height:100%; 
                            object-fit:contain; 
                        }
                    </style>
                </head>
                <body>
                    <img src="${img.src}" alt="Full size image">
                </body>
            </html>
        `);
    }
});

document.body.addEventListener('htmx:afterSettle', function(evt) {
    if (evt.target && (evt.target.id === 'file-input' || evt.target.name === 'image')) {
        initializeFilePreview();
    }
    
    setupDeleteButtons();
});

/*
function setupDeleteButtons() {
    const userIdElement = document.querySelector('.user-info .user-id');
    if (!userIdElement) {
        console.log('Could not find current user ID element');
        return;
    }
    
    const currentUserShortId = userIdElement.textContent.trim();
    console.log('Current user short ID:', currentUserShortId);
    
    const deleteButtons = document.querySelectorAll('.delete-message-btn');
    console.log('Found delete buttons:', deleteButtons.length);
    
    deleteButtons.forEach(button => {
        const message = button.closest('.message');
        if (message) {
            const messageUserId = message.dataset.userId;
            const messageUserShortId = messageUserId ? messageUserId.substring(0, 4) : '';
            
            console.log('Message user ID:', messageUserId, 'Short:', messageUserShortId);
            console.log('Current user short ID:', currentUserShortId);
            console.log('Match:', messageUserShortId === currentUserShortId);
            
            if (messageUserShortId === currentUserShortId) {
                button.style.display = 'none';
                button.dataset.isOwn = 'true';
            } else {
                button.remove();
            }
        }
    });
}
    */